package confload

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

var (
	errUnsupportedConfigFormat = errors.New("unsupported config file format")
	errConfigFileIsDir         = errors.New("config file path must be a file")
	errNilLoader              = errors.New("loader is nil")
)

const defaultTagName = "k"

// Loader aggregates configuration from defaults, files, environment variables and flags.
type Loader struct {
	opts         options
	lastSnapshot Snapshot
}

// SourceType describes where a configuration value originated from.
type SourceType string

// Known configuration value sources.
const (
	SourceStructDefault SourceType = "struct_default"
	SourceDefaults      SourceType = "defaults"
	SourceFile          SourceType = "file"
	SourceReader        SourceType = "reader"
	SourceEnv           SourceType = "env"
	SourceFlag          SourceType = "flag"
	SourceOverride      SourceType = "override"
)

// ValueOrigin provides provenance details for a configuration key.
type ValueOrigin struct {
	Source     SourceType
	Identifier string
}

// Snapshot exposes the latest flattened configuration and origin metadata.
type Snapshot struct {
	Values  map[string]any
	Origins map[string]ValueOrigin
	Files   []string
}

func (s Snapshot) clone() Snapshot {
	values := make(map[string]any, len(s.Values))
	for k, v := range s.Values {
		values[k] = v
	}

	origins := make(map[string]ValueOrigin, len(s.Origins))
	for k, v := range s.Origins {
		origins[k] = v
	}

	files := append([]string(nil), s.Files...)

	return Snapshot{Values: values, Origins: origins, Files: files}
}

// EnvKeyFunc builds the environment variable name for a dotted key.
type EnvKeyFunc func(prefix, dotted string) string

type options struct {
	EnvPrefix     string
	TagName       string
	EnvKeyFunc    EnvKeyFunc
	EnvAliases    map[string]string
	ConfigFileEnv string
	Defaults      map[string]any
	Sources       []sourceEntry
	FlagSet       *flag.FlagSet
	Overrides     map[string]any
	DecodeHooks   []mapstructure.DecodeHookFunc
	Strict        bool
}

type sourceKind uint8

const (
	sourceFile sourceKind = iota
	sourceReader
)

type sourceEntry struct {
	kind     sourceKind
	path     string
	optional bool
	data     []byte
	readErr  error
}

// Option configures Loader behavior.
type Option func(*options)

// WithEnvPrefix configures the prefix applied to environment variables.
func WithEnvPrefix(prefix string) Option {
	return func(o *options) {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return
		}
		if !strings.HasSuffix(prefix, "_") {
			prefix += "_"
		}
		o.EnvPrefix = prefix
	}
}

// WithTagName sets the struct tag key used for configuration fields.
func WithTagName(tag string) Option {
	return func(o *options) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		o.TagName = tag
	}
}

// WithEnvKeyFunc overrides the environment variable name mapping for dotted keys.
func WithEnvKeyFunc(fn EnvKeyFunc) Option {
	return func(o *options) {
		if fn == nil {
			return
		}
		o.EnvKeyFunc = fn
	}
}

// WithEnvAliases sets explicit environment variable names for specific dotted keys.
func WithEnvAliases(aliases map[string]string) Option {
	return func(o *options) {
		if len(aliases) == 0 {
			return
		}

		o.EnvAliases = cloneStringMap(aliases)
	}
}

// WithConfigFileEnv sets the environment variable key used to locate configuration files.
func WithConfigFileEnv(key string) Option {
	return func(o *options) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		o.ConfigFileEnv = key
	}
}

// WithDefaults supplies default values expressed using dotted keys.
func WithDefaults(values map[string]any) Option {
	return func(o *options) {
		if len(values) == 0 {
			return
		}

		if o.Defaults == nil {
			o.Defaults = make(map[string]any, len(values))
		}

		for k, v := range values {
			o.Defaults[k] = v
		}
	}
}

// WithFiles adds configuration files that will be loaded in the provided order.
func WithFiles(paths ...string) Option {
	return func(o *options) {
		if len(paths) == 0 {
			return
		}

		for _, path := range paths {
			o.Sources = append(o.Sources, sourceEntry{kind: sourceFile, path: path})
		}
	}
}

// WithOptionalFiles adds configuration files that will be loaded if they exist.
func WithOptionalFiles(paths ...string) Option {
	return func(o *options) {
		if len(paths) == 0 {
			return
		}

		for _, path := range paths {
			o.Sources = append(o.Sources, sourceEntry{kind: sourceFile, path: path, optional: true})
		}
	}
}

// WithReader loads configuration from an io.Reader using the file extension of name.
// The reader is consumed when the option is applied, and read errors surface during load.
func WithReader(name string, reader io.Reader) Option {
	return func(o *options) {
		name = strings.TrimSpace(name)
		if name == "" || reader == nil {
			return
		}

		data, err := io.ReadAll(reader)
		o.Sources = append(o.Sources, sourceEntry{
			kind:    sourceReader,
			path:    name,
			data:    data,
			readErr: err,
		})
	}
}

// WithFlagSet enables flag values as a configuration source.
func WithFlagSet(fs *flag.FlagSet) Option {
	return func(o *options) {
		o.FlagSet = fs
	}
}

// WithOverrides applies programmatic overrides expressed using dotted keys.
func WithOverrides(values map[string]any) Option {
	return func(o *options) {
		if len(values) == 0 {
			return
		}

		if o.Overrides == nil {
			o.Overrides = make(map[string]any, len(values))
		}
		for k, v := range values {
			o.Overrides[k] = v
		}
	}
}

// WithDecodeHooks registers additional decode hooks applied after the defaults.
func WithDecodeHooks(hooks ...mapstructure.DecodeHookFunc) Option {
	return func(o *options) {
		if len(hooks) == 0 {
			return
		}

		o.DecodeHooks = append(o.DecodeHooks, hooks...)
	}
}

// WithStrict enables validation that rejects unknown configuration keys.
func WithStrict() Option {
	return func(o *options) {
		o.Strict = true
	}
}

// New constructs a configuration loader with the supplied options applied.
func New(opts ...Option) *Loader {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	return &Loader{opts: o}
}

// LoadInto unmarshals the entire configuration into the provided type.
func LoadInto[T any](loader *Loader) (*T, error) {
	if loader == nil {
		return nil, errNilLoader
	}

	var target T
	if err := loader.Unmarshal(&target); err != nil {
		return nil, err
	}

	return &target, nil
}

// LoadIntoWithSnapshot loads configuration and returns the snapshot collected during the load.
func LoadIntoWithSnapshot[T any](loader *Loader) (*T, Snapshot, error) {
	if loader == nil {
		return nil, Snapshot{}, errNilLoader
	}

	var target T
	if err := loader.Unmarshal(&target); err != nil {
		return nil, Snapshot{}, err
	}

	return &target, loader.Snapshot(), nil
}

// LoadSubset unmarshals a configuration subtree rooted at the given prefix into the provided type.
func LoadSubset[T any](loader *Loader, prefix string) (*T, error) {
	if loader == nil {
		return nil, errNilLoader
	}

	var target T
	if err := loader.UnmarshalPrefix(prefix, &target); err != nil {
		return nil, err
	}

	return &target, nil
}

// Unmarshal populates target with the entire configuration tree.
func (l *Loader) Unmarshal(target any) error {
	if l == nil {
		return errNilLoader
	}

	return l.unmarshal("", target)
}

// UnmarshalPrefix populates target with configuration rooted at prefix.
func (l *Loader) UnmarshalPrefix(prefix string, target any) error {
	if l == nil {
		return errNilLoader
	}

	return l.unmarshal(prefix, target)
}

// EnvName returns the environment variable name for a dotted path using loader options.
func (l *Loader) EnvName(dotted string) string {
	if l == nil {
		return ""
	}

	return envNameForKey(l.opts.EnvPrefix, dotted, l.opts.EnvKeyFunc, l.opts.EnvAliases)
}

// Snapshot returns the most recently captured configuration snapshot.
func (l *Loader) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{}
	}

	if len(l.lastSnapshot.Values) == 0 && len(l.lastSnapshot.Origins) == 0 {
		return Snapshot{}
	}

	return l.lastSnapshot.clone()
}

func (l *Loader) unmarshal(prefix string, target any) error {
	rv := reflect.ValueOf(target)

	targetType := rv.Type().Elem()
	tagName := normalizeTagName(l.opts.TagName)
	keys := collectTaggedKeys(targetType, prefix, tagName)

	// Defaults from struct tags have the lowest priority.
	tagDefaults := collectTagDefaults(targetType, prefix, tagName)

	var allowedKeys map[string]struct{}
	var wildcardPrefixes []string
	if l.opts.Strict {
		allowedKeys = collectAllowedKeys(targetType, prefix, tagName)
		wildcardPrefixes = collectMapPrefixes(targetType, prefix, tagName)
	}

	k, err := l.snapshot(keys, tagDefaults, allowedKeys, wildcardPrefixes, prefix)
	if err != nil {
		return err
	}

	hooks := []mapstructure.DecodeHookFunc{
		mapstructure.StringToTimeDurationHookFunc(),
		stringToBoolHook,
		stringToNumberHook,
		stringToFloatHook,
		stringToURLHook,
	}
	if len(l.opts.DecodeHooks) > 0 {
		hooks = append(hooks, l.opts.DecodeHooks...)
	}
	decodeHook := mapstructure.ComposeDecodeHookFunc(hooks...)

	unmarshalConf := koanf.UnmarshalConf{
		Tag: tagName,
		DecoderConfig: &mapstructure.DecoderConfig{
			TagName:    tagName,
			Result:     target,
			DecodeHook: decodeHook,
		},
	}

	if err := k.UnmarshalWithConf(prefix, target, unmarshalConf); err != nil {
		scope := prefix
		if scope == "" {
			scope = "configuration"
		}

		return fmt.Errorf("unmarshal %s: %w", scope, err)
	}

	return nil
}

func (l *Loader) snapshot(
	keys []string,
	tagDefaults map[string]any,
	allowedKeys map[string]struct{},
	wildcardPrefixes []string,
	prefix string,
) (*koanf.Koanf, error) {
	k := koanf.New(".")
	origins := make(map[string]ValueOrigin)
	files := make([]string, 0)

	if err := l.loadStructTagDefaults(k, tagDefaults, origins); err != nil {
		return nil, err
	}

	if err := l.loadDefaults(k, origins); err != nil {
		return nil, err
	}

	loadedFiles, err := l.loadSources(k, origins)
	if err != nil {
		return nil, err
	}
	files = append(files, loadedFiles...)

	if err := l.loadEnv(k, keys, origins); err != nil {
		return nil, err
	}

	if err := l.loadFlags(k, origins); err != nil {
		return nil, err
	}

	if err := l.loadOverrides(k, origins); err != nil {
		return nil, err
	}

	if allowedKeys != nil {
		if err := validateStrict(k, origins, allowedKeys, wildcardPrefixes, prefix); err != nil {
			return nil, err
		}
	}

	values := make(map[string]any, len(keys))
	for _, key := range keys {
		if val := k.Get(key); val != nil {
			values[key] = val

			continue
		}
		if _, ok := origins[key]; ok {
			values[key] = k.Get(key)
		}
	}

	l.lastSnapshot = Snapshot{
		Values:  values,
		Origins: origins,
		Files:   uniqueStrings(files),
	}

	return k, nil
}

func (l *Loader) loadStructTagDefaults(k *koanf.Koanf, values map[string]any, origins map[string]ValueOrigin) error {
	if len(values) == 0 {
		return nil
	}

	defaults := cloneMap(values)
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return fmt.Errorf("load struct tag defaults: %w", err)
	}
	recordOrigins(defaults, ValueOrigin{Source: SourceStructDefault}, origins)

	return nil
}

func (l *Loader) loadDefaults(k *koanf.Koanf, origins map[string]ValueOrigin) error {
	if len(l.opts.Defaults) == 0 {
		return nil
	}

	defaults := cloneMap(l.opts.Defaults)
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return fmt.Errorf("load default configuration: %w", err)
	}
	recordOrigins(defaults, ValueOrigin{Source: SourceDefaults}, origins)

	return nil
}

func (l *Loader) loadSources(k *koanf.Koanf, origins map[string]ValueOrigin) ([]string, error) {
	loaded := make([]string, 0, len(l.opts.Sources)+1)
	for _, source := range l.opts.Sources {
		switch source.kind {
		case sourceFile:
			ok, err := loadFile(k, source.path, origins, source.optional)
			if err != nil {
				return nil, err
			}
			if ok {
				loaded = append(loaded, filepath.Clean(strings.TrimSpace(source.path)))
			}
		case sourceReader:
			name := strings.TrimSpace(source.path)
			if name == "" {
				continue
			}
			if source.readErr != nil {
				return nil, fmt.Errorf("read config reader %q: %w", name, source.readErr)
			}
			ok, err := loadReader(k, name, source.data, origins)
			if err != nil {
				return nil, err
			}
			if ok {
				loaded = append(loaded, name)
			}
		}
	}

	if envPath := strings.TrimSpace(os.Getenv(l.opts.ConfigFileEnv)); envPath != "" {
		ok, err := loadFile(k, envPath, origins, false)
		if err != nil {
			return nil, err
		}
		if ok {
			loaded = append(loaded, filepath.Clean(envPath))
		}
	}

	return loaded, nil
}

func loadFile(k *koanf.Koanf, path string, origins map[string]ValueOrigin, optional bool) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}

	cleanPath := filepath.Clean(path)
	info, statErr := os.Stat(cleanPath)
	if statErr != nil {
		if optional && os.IsNotExist(statErr) {
			return false, nil
		}
		return false, fmt.Errorf("stat config file %q: %w", cleanPath, statErr)
	}
	if info.IsDir() {
		return false, fmt.Errorf("config file %q: %w", cleanPath, errConfigFileIsDir)
	}

	parser, err := parserForPath(cleanPath)
	if err != nil {
		return false, err
	}

	if err := k.Load(file.Provider(cleanPath), parser); err != nil {
		return false, fmt.Errorf("load config file %q: %w", cleanPath, err)
	}

	if origins != nil {
		kFile := koanf.New(".")
		if err := kFile.Load(file.Provider(cleanPath), parser); err == nil {
			recordOrigins(flattenKoanf(kFile), ValueOrigin{Source: SourceFile, Identifier: cleanPath}, origins)
		}
	}

	return true, nil
}

func loadReader(k *koanf.Koanf, name string, data []byte, origins map[string]ValueOrigin) (bool, error) {
	parser, err := parserForPath(name)
	if err != nil {
		return false, err
	}

	if err := k.Load(rawbytes.Provider(data), parser); err != nil {
		return false, fmt.Errorf("load config reader %q: %w", name, err)
	}

	if origins != nil {
		kReader := koanf.New(".")
		if err := kReader.Load(rawbytes.Provider(data), parser); err == nil {
			recordOrigins(flattenKoanf(kReader), ValueOrigin{Source: SourceReader, Identifier: name}, origins)
		}
	}

	return true, nil
}

func parserForPath(path string) (koanf.Parser, error) {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".yaml", ".yml":
		return yaml.Parser(), nil
	case ".json":
		return json.Parser(), nil
	case ".toml":
		return toml.Parser(), nil
	default:
		if ext == "" {
			ext = "unknown"
		}

		return nil, fmt.Errorf("%w: %s", errUnsupportedConfigFormat, ext)
	}
}

func (l *Loader) loadFlags(k *koanf.Koanf, origins map[string]ValueOrigin) error {
	if l.opts.FlagSet == nil {
		return nil
	}

	values := make(map[string]any)
	identifiers := make(map[string]string)
	l.opts.FlagSet.Visit(func(f *flag.Flag) {
		dotted := flagKeyToDotted(f.Name)
		if provider, ok := f.Value.(interface{ DottedKey() string }); ok {
			if dk := strings.TrimSpace(provider.DottedKey()); dk != "" {
				dotted = dk
			}
		}
		if dotted == "" {
			return
		}

		values[dotted] = f.Value.String()
		identifiers[dotted] = f.Name
	})

	if len(values) == 0 {
		return nil
	}

	if err := k.Load(confmap.Provider(values, "."), nil); err != nil {
		return fmt.Errorf("load flag configuration: %w", err)
	}
	for key := range values {
		origin := ValueOrigin{Source: SourceFlag, Identifier: identifiers[key]}
		origins[key] = origin
	}

	return nil
}

func (l *Loader) loadEnv(k *koanf.Koanf, keys []string, origins map[string]ValueOrigin) error {
	if len(keys) == 0 {
		return nil
	}

	values := readEnvExact(l.opts.EnvPrefix, keys, l.opts.EnvKeyFunc, l.opts.EnvAliases)
	if len(values) == 0 {
		return nil
	}

	if err := k.Load(confmap.Provider(values, "."), nil); err != nil {
		return fmt.Errorf("load environment configuration: %w", err)
	}
	for key := range values {
		origins[key] = ValueOrigin{
			Source:     SourceEnv,
			Identifier: envNameForKey(l.opts.EnvPrefix, key, l.opts.EnvKeyFunc, l.opts.EnvAliases),
		}
	}

	return nil
}

func (l *Loader) loadOverrides(k *koanf.Koanf, origins map[string]ValueOrigin) error {
	if len(l.opts.Overrides) == 0 {
		return nil
	}

	overrides := cloneMap(l.opts.Overrides)
	if err := k.Load(confmap.Provider(overrides, "."), nil); err != nil {
		return fmt.Errorf("apply overrides: %w", err)
	}
	recordOrigins(overrides, ValueOrigin{Source: SourceOverride}, origins)

	return nil
}

func collectTaggedKeys(t reflect.Type, prefix, tagName string) []string {
	acc := make(map[string]struct{})
	collectKeys(t, prefix, tagName, acc)

	out := make([]string, 0, len(acc))
	for key := range acc {
		out = append(out, key)
	}
	sort.Strings(out)

	return out
}

func collectAllowedKeys(t reflect.Type, prefix, tagName string) map[string]struct{} {
	keys := collectTaggedKeys(t, prefix, tagName)
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
		for parent := parentPath(key); parent != ""; parent = parentPath(parent) {
			out[parent] = struct{}{}
		}
	}

	return out
}

func collectMapPrefixes(t reflect.Type, prefix, tagName string) []string {
	acc := make(map[string]struct{})
	collectMapPrefixesInto(t, prefix, tagName, acc)

	out := make([]string, 0, len(acc))
	for key := range acc {
		out = append(out, key)
	}
	sort.Strings(out)

	return out
}

func collectKeys(t reflect.Type, prefix, tagName string, acc map[string]struct{}) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		if prefix != "" {
			acc[prefix] = struct{}{}
		}

		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.TrimSpace(f.Tag.Get(tagName))
		if tag == "" || tag == "-" {
			continue
		}

		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}

		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		if ft.Kind() == reflect.Struct && ft.PkgPath() != netURLPkg {
			collectKeys(ft, key, tagName, acc)

			continue
		}

		acc[key] = struct{}{}
	}
}

func collectMapPrefixesInto(t reflect.Type, prefix, tagName string, acc map[string]struct{}) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.TrimSpace(f.Tag.Get(tagName))
		if tag == "" || tag == "-" {
			continue
		}

		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}

		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		if ft.Kind() == reflect.Map {
			acc[key] = struct{}{}
			continue
		}

		if ft.Kind() == reflect.Struct && ft.PkgPath() != netURLPkg {
			collectMapPrefixesInto(ft, key, tagName, acc)
		}
	}
}

// collectTagDefaults traverses the struct fields and collects tagged defaults.
func collectTagDefaults(t reflect.Type, prefix, tagName string) map[string]any {
	acc := make(map[string]any)
	collectTagDefaultsInto(t, prefix, tagName, acc)

	return acc
}

func collectTagDefaultsInto(t reflect.Type, prefix, tagName string, acc map[string]any) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.TrimSpace(f.Tag.Get(tagName))
		if tag == "" || tag == "-" {
			continue
		}

		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}

		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		// net/url.URL contains internal fields, but for us it is a leaf.
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "net/url" {
			collectTagDefaultsInto(ft, key, tagName, acc)

			continue
		}

		defVal, ok := f.Tag.Lookup("default")
		if !ok {
			continue
		}

		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			sep := f.Tag.Get("default_sep")
			if sep == "" {
				sep = ","
			}
			parts := strings.Split(defVal, sep)
			list := make([]any, 0, len(parts))
			for _, p := range parts {
				list = append(list, strings.TrimSpace(p))
			}

			acc[key] = list

			continue
		}

		acc[key] = defVal
	}
}

func envName(prefix, dotted string) string {
	replacer := strings.NewReplacer(".", "_")

	return prefix + strings.ToUpper(replacer.Replace(dotted))
}

func normalizeTagName(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return defaultTagName
	}

	return tag
}

func envNameForKey(prefix, dotted string, fn EnvKeyFunc, aliases map[string]string) string {
	if len(aliases) > 0 {
		if alias, ok := aliases[dotted]; ok {
			return strings.TrimSpace(alias)
		}
	}

	if fn != nil {
		return strings.TrimSpace(fn(prefix, dotted))
	}

	return envName(prefix, dotted)
}

func readEnvExact(prefix string, keys []string, fn EnvKeyFunc, aliases map[string]string) map[string]any {
	out := make(map[string]any)
	for _, key := range keys {
		envKey := envNameForKey(prefix, key, fn, aliases)
		if envKey == "" {
			continue
		}

		if val, ok := os.LookupEnv(envKey); ok {
			out[key] = val

			continue
		}

		if list := readEnvList(envKey); len(list) > 0 {
			out[key] = list

			continue
		}
	}

	return out
}

func readEnvList(base string) []any {
	values := make([]any, 0)
	for idx := 0; ; idx++ {
		name := fmt.Sprintf("%s_%d", base, idx)
		val, ok := os.LookupEnv(name)
		if !ok {
			if idx == 0 {
				return nil
			}

			break
		}
		values = append(values, val)
	}

	return values
}

func flagKeyToDotted(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	replacer := strings.NewReplacer("-", ".", "__", ".", "_", ".")
	trimmed = replacer.Replace(trimmed)
	trimmed = strings.Trim(trimmed, ".")

	return strings.ToLower(trimmed)
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}

	return dst
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}

	return dst
}

func recordOrigins(values map[string]any, origin ValueOrigin, origins map[string]ValueOrigin) {
	if origins == nil || len(values) == 0 {
		return
	}

	for key := range values {
		origins[key] = origin

		for parent := parentPath(key); parent != ""; parent = parentPath(parent) {
			if _, exists := origins[parent]; exists {
				continue
			}

			origins[parent] = origin
		}
	}
}

func parentPath(path string) string {
	if path == "" {
		return ""
	}

	idx := strings.LastIndex(path, ".")
	if idx <= 0 {
		return ""
	}

	return path[:idx]
}

func flattenKoanf(k *koanf.Koanf) map[string]any {
	keys := k.Keys()
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		out[key] = k.Get(key)
	}

	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	uniq := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		uniq = append(uniq, v)
	}

	return uniq
}

const netURLPkg = "net/url"
