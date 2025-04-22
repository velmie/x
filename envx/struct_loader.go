package envx

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Tag struct {
	Names      []string
	Directives []Directive
}

type Directive struct {
	Name   string
	Params []string
}

type TagParser interface {
	Parse(tag string) (Tag, error)
}

type Option func(*StructLoader)

func WithPrefix(prefix string) Option {
	return func(l *StructLoader) {
		l.prefix = prefix
	}
}

func WithPrefixFallback(enable bool) Option {
	return func(l *StructLoader) {
		l.enablePrefixFallback = enable
	}
}

func WithTagParser(parser TagParser) Option {
	return func(l *StructLoader) {
		l.tagParser = parser
	}
}

func WithCustomValidator(name string, validator DirectiveHandler) Option {
	return func(l *StructLoader) {
		l.directiveHandlers[name] = validator
	}
}

func WithTypeHandler(typ reflect.Type, handler TypeHandler) Option {
	return func(l *StructLoader) {
		l.typeHandlers[typ] = handler
	}
}

func WithKindHandler(kind reflect.Kind, handler KindHandler) Option {
	return func(l *StructLoader) {
		l.kindHandlers[kind] = handler
	}
}

func Load(cfg any, opts ...Option) error {
	loader := NewStructLoader(opts...)
	return loader.Load(cfg)
}

type StructLoader struct {
	prefix               string
	enablePrefixFallback bool
	tagParser            TagParser
	typeHandlers         map[reflect.Type]TypeHandler
	kindHandlers         map[reflect.Kind]KindHandler
	directiveHandlers    map[string]DirectiveHandler
	resolver             Resolver
}

func WithResolver(resolver Resolver) Option {
	return func(l *StructLoader) {
		l.resolver = resolver
	}
}

func NewStructLoader(opts ...Option) *StructLoader {
	loader := &StructLoader{
		prefix:               "",
		enablePrefixFallback: false,
		tagParser:            NewTagParser(),
		typeHandlers:         make(map[reflect.Type]TypeHandler),
		kindHandlers:         make(map[reflect.Kind]KindHandler),
		directiveHandlers:    make(map[string]DirectiveHandler),
		resolver:             DefaultResolver,
	}

	loader.typeHandlers[reflect.TypeOf(time.Time{})] = &TimeHandler{}
	loader.typeHandlers[reflect.TypeOf(&url.URL{})] = &URLHandler{}

	loader.kindHandlers[reflect.String] = &StringHandler{}
	loader.kindHandlers[reflect.Bool] = &BoolHandler{}
	loader.kindHandlers[reflect.Int] = &IntHandler{}
	loader.kindHandlers[reflect.Int8] = &IntHandler{}
	loader.kindHandlers[reflect.Int16] = &IntHandler{}
	loader.kindHandlers[reflect.Int32] = &IntHandler{}
	loader.kindHandlers[reflect.Int64] = &Int64Handler{}
	loader.kindHandlers[reflect.Uint] = &UintHandler{}
	loader.kindHandlers[reflect.Uint8] = &UintHandler{}
	loader.kindHandlers[reflect.Uint16] = &UintHandler{}
	loader.kindHandlers[reflect.Uint32] = &UintHandler{}
	loader.kindHandlers[reflect.Uint64] = &UintHandler{}
	loader.kindHandlers[reflect.Float32] = &FloatHandler{}
	loader.kindHandlers[reflect.Float64] = &FloatHandler{}
	loader.kindHandlers[reflect.Slice] = &SliceHandler{}
	loader.kindHandlers[reflect.Map] = &MapHandler{}
	loader.kindHandlers[reflect.Struct] = &StructHandler{}
	loader.kindHandlers[reflect.Ptr] = &PointerHandler{}

	loader.directiveHandlers["required"] = RequiredDirectiveHandler
	loader.directiveHandlers["requiredIfMethod"] = RequiredIfMethodDirectiveHandler
	loader.directiveHandlers["notEmpty"] = NotEmptyDirectiveHandler
	loader.directiveHandlers["default"] = DefaultDirectiveHandler
	loader.directiveHandlers["expand"] = ExpandDirectiveHandler
	loader.directiveHandlers["validURL"] = ValidURLDirectiveHandler
	loader.directiveHandlers["validIP"] = ValidIPDirectiveHandler
	loader.directiveHandlers["validPort"] = ValidPortDirectiveHandler
	loader.directiveHandlers["validDomain"] = ValidDomainDirectiveHandler
	loader.directiveHandlers["validListenAddr"] = ValidListenAddrDirectiveHandler
	loader.directiveHandlers["minLen"] = MinLenDirectiveHandler
	loader.directiveHandlers["maxLen"] = MaxLenDirectiveHandler
	loader.directiveHandlers["exactLen"] = ExactLenDirectiveHandler

	loader.directiveHandlers["min"] = MinDirectiveHandler
	loader.directiveHandlers["max"] = MaxDirectiveHandler
	loader.directiveHandlers["range"] = RangeDirectiveHandler

	loader.directiveHandlers["regexp"] = RegexpDirectiveHandler
	loader.directiveHandlers["oneOf"] = OneOfDirectiveHandler

	for _, opt := range opts {
		opt(loader)
	}

	return loader
}

func (l *StructLoader) Load(cfg any) error {
	val := reflect.ValueOf(cfg)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("cfg must be a pointer to a struct")
	}

	val = val.Elem()
	typ := val.Type()

	var errs []error

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		if !field.IsExported() || !fieldVal.CanSet() {
			continue
		}

		ctx, err := l.createFieldContext(cfg, field, fieldVal)
		if err != nil {
			errs = append(errs, fmt.Errorf("creating context for field %s: %w", field.Name, err))
			continue
		}

		err = l.applyDirectives(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("applying directives for field %s: %w", field.Name, err))
			continue
		}

		err = l.setFieldValue(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

type FieldContext struct {
	Target               any
	Field                reflect.StructField
	FieldValue           reflect.Value
	FinalNames           []string
	Directives           []Directive
	ValidateMethod       string
	ConvertMethod        string
	TimeLayout           string
	Delimiter            string
	Variable             *Variable
	Prefix               string
	EnablePrefixFallback bool
	TagParser            TagParser
	SearchPlan           SearchPlan
	Resolver             Resolver
}

func (l *StructLoader) createFieldContext(cfg any, field reflect.StructField, fieldVal reflect.Value) (*FieldContext, error) {
	ctx := &FieldContext{
		Target:               cfg,
		Field:                field,
		FieldValue:           fieldVal,
		Delimiter:            ",",
		TimeLayout:           time.RFC3339,
		Prefix:               l.prefix,
		EnablePrefixFallback: l.enablePrefixFallback,
		TagParser:            l.tagParser,
		Resolver:             l.resolver,
	}

	tagValue, hasTag := field.Tag.Lookup("env")
	if hasTag {
		tag, err := l.tagParser.Parse(tagValue)
		if err != nil {
			return nil, fmt.Errorf("invalid tag: %w", err)
		}
		ctx.Directives = tag.Directives

		for _, dir := range ctx.Directives {
			switch dir.Name {
			case "delimiter":
				if len(dir.Params) == 1 {
					ctx.Delimiter = dir.Params[0]
				}
			case "layout":
				if len(dir.Params) == 1 {
					ctx.TimeLayout = dir.Params[0]
				}
			case "validateMethod":
				if len(dir.Params) == 1 {
					ctx.ValidateMethod = dir.Params[0]
				}
			case "convertMethod":
				if len(dir.Params) == 1 {
					ctx.ConvertMethod = dir.Params[0]
				}
			}
		}
	}

	plan, err := l.parseSearchPlan(field, tagValue)
	if err != nil {
		return nil, fmt.Errorf("invalid search plan: %w", err)
	}
	ctx.SearchPlan = plan

	return ctx, nil
}

type DirectiveHandler func(ctx *FieldContext, dir Directive) error

func (l *StructLoader) applyDirectives(ctx *FieldContext) error {
	var err error

	if len(ctx.SearchPlan.Steps) > 0 {
		ctx.Variable, err = ctx.Resolver.ResolvePlan(ctx.SearchPlan)
	} else {
		ctx.Variable, err = ctx.Resolver.Coalesce(ctx.FinalNames...)
	}

	if err != nil {
		return fmt.Errorf("error resolving variables: %w", err)
	}

	if len(ctx.FinalNames) > 0 {
		ctx.Variable.AllNames = make([]string, len(ctx.FinalNames))
		copy(ctx.Variable.AllNames, ctx.FinalNames)
	}

	for _, dir := range ctx.Directives {
		if dir.Name == "delimiter" || dir.Name == "layout" || dir.Name == "validateMethod" || dir.Name == "convertMethod" {
			continue
		}

		handler, ok := l.directiveHandlers[dir.Name]
		if !ok {
			return fmt.Errorf("unknown directive %q", dir.Name)
		}

		if err := handler(ctx, dir); err != nil {
			return err
		}
	}

	return nil
}

func (l *StructLoader) setFieldValue(ctx *FieldContext) error {
	fieldType := ctx.FieldValue.Type()
	convertedVal := reflect.New(fieldType).Elem()

	if ctx.ConvertMethod != "" {
		rawValue, err := ctx.Variable.String()
		if err != nil {
			return err
		}

		value, err := CallConvertMethod(ctx.Target, ctx.ConvertMethod, rawValue, fieldType)
		if err != nil {
			return fmt.Errorf("conversion failed: %w", err)
		}
		convertedVal.Set(reflect.ValueOf(value))
	} else if handler, ok := l.typeHandlers[fieldType]; ok {
		value, err := handler.HandleType(ctx)
		if err != nil {
			return err
		}
		convertedVal.Set(reflect.ValueOf(value))
	} else if fieldType == reflect.TypeOf(time.Duration(0)) {
		value, err := ctx.Variable.Duration()
		if err != nil {
			return err
		}
		convertedVal.SetInt(int64(value))
	} else {
		if handler, ok := l.kindHandlers[fieldType.Kind()]; ok {
			value, err := handler.HandleKind(ctx)
			if err != nil {
				return err
			}
			convertedVal.Set(reflect.ValueOf(value))
		} else {
			return fmt.Errorf("unsupported field type %s", fieldType)
		}
	}

	if ctx.ValidateMethod != "" {
		err := CallValidateMethod(ctx.Target, ctx.ValidateMethod, convertedVal.Interface())
		if err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	ctx.FieldValue.Set(convertedVal)
	return nil
}

func RequiredDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.Required()
	return nil
}

func RequiredIfMethodDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("requiredIfMethod needs exactly one parameter")
	}

	methodName := dir.Params[0]
	result, err := CallBoolMethod(ctx.Target, methodName)
	if err != nil {
		return fmt.Errorf("error calling requiredIfMethod %s: %w", methodName, err)
	}

	if result {
		ctx.Variable = ctx.Variable.Required()
	}

	return nil
}

func NotEmptyDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.NotEmpty()
	return nil
}

func DefaultDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) == 0 {
		ctx.Variable = ctx.Variable.Default("")
		return nil
	}

	if len(dir.Params) > 1 {
		return fmt.Errorf("default accepts at most one parameter")
	}

	ctx.Variable = ctx.Variable.Default(dir.Params[0])
	return nil
}

func ExpandDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.Expand()
	return nil
}

func ValidURLDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.ValidURL()
	return nil
}

func ValidIPDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.ValidIPAddress()
	return nil
}

func ValidPortDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.ValidPortNumber()
	return nil
}

func ValidDomainDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.ValidDomainName()
	return nil
}

func ValidListenAddrDirectiveHandler(ctx *FieldContext, _ Directive) error {
	ctx.Variable = ctx.Variable.ValidListenAddress()
	return nil
}

func MinLenDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("minLen needs exactly one parameter")
	}

	val, err := parseIntParam(dir.Params[0])
	if err != nil {
		return fmt.Errorf("minLen parameter must be an integer: %w", err)
	}

	ctx.Variable = ctx.Variable.MinLength(val)
	return nil
}

func MaxLenDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("maxLen needs exactly one parameter")
	}

	val, err := parseIntParam(dir.Params[0])
	if err != nil {
		return fmt.Errorf("maxLen parameter must be an integer: %w", err)
	}

	ctx.Variable = ctx.Variable.MaxLength(val)
	return nil
}

func ExactLenDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("exactLen needs exactly one parameter")
	}

	val, err := parseIntParam(dir.Params[0])
	if err != nil {
		return fmt.Errorf("exactLen parameter must be an integer: %w", err)
	}

	ctx.Variable = ctx.Variable.ExactLength(val)
	return nil
}

func MinIntDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("minInt needs exactly one parameter")
	}

	val, err := parseInt64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("minInt parameter must be an integer: %w", err)
	}

	ctx.Variable = ctx.Variable.MinInt(val)
	return nil
}

func MaxIntDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("maxInt needs exactly one parameter")
	}

	val, err := parseInt64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("maxInt parameter must be an integer: %w", err)
	}

	ctx.Variable = ctx.Variable.MaxInt(val)
	return nil
}

func MinUintDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("minUint needs exactly one parameter")
	}

	val, err := parseUint64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("minUint parameter must be an unsigned integer: %w", err)
	}

	ctx.Variable = ctx.Variable.MinUint(val)
	return nil
}

func MaxUintDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("maxUint needs exactly one parameter")
	}

	val, err := parseUint64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("maxUint parameter must be an unsigned integer: %w", err)
	}

	ctx.Variable = ctx.Variable.MaxUint(val)
	return nil
}

func MinFloatDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("minFloat needs exactly one parameter")
	}

	val, err := parseFloat64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("minFloat parameter must be a float: %w", err)
	}

	ctx.Variable = ctx.Variable.MinFloat(val)
	return nil
}

func MaxFloatDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("maxFloat needs exactly one parameter")
	}

	val, err := parseFloat64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("maxFloat parameter must be a float: %w", err)
	}

	ctx.Variable = ctx.Variable.MaxFloat(val)
	return nil
}

func RangeIntDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 2 {
		return fmt.Errorf("rangeInt needs exactly two parameters")
	}

	mn, err := parseInt64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("rangeInt min parameter must be an integer: %w", err)
	}

	mx, err := parseInt64Param(dir.Params[1])
	if err != nil {
		return fmt.Errorf("rangeInt max parameter must be an integer: %w", err)
	}

	ctx.Variable = ctx.Variable.IntRange(mn, mx)
	return nil
}

func RangeUintDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 2 {
		return fmt.Errorf("rangeUint needs exactly two parameters")
	}

	mn, err := parseUint64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("rangeUint min parameter must be an unsigned integer: %w", err)
	}

	mx, err := parseUint64Param(dir.Params[1])
	if err != nil {
		return fmt.Errorf("rangeUint max parameter must be an unsigned integer: %w", err)
	}

	ctx.Variable = ctx.Variable.UintRange(mn, mx)
	return nil
}

func RangeFloatDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 2 {
		return fmt.Errorf("rangeFloat needs exactly two parameters")
	}

	mn, err := parseFloat64Param(dir.Params[0])
	if err != nil {
		return fmt.Errorf("rangeFloat min parameter must be a float: %w", err)
	}

	mx, err := parseFloat64Param(dir.Params[1])
	if err != nil {
		return fmt.Errorf("rangeFloat max parameter must be a float: %w", err)
	}

	ctx.Variable = ctx.Variable.FloatRange(mn, mx)
	return nil
}

func MinDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("min needs exactly one parameter")
	}

	fieldType := ctx.FieldValue.Type()
	param := dir.Params[0]

	switch fieldType.Kind() {
	case reflect.String:
		val, err := parseIntParam(param)
		if err != nil {
			return fmt.Errorf("min parameter must be an integer for string length: %w", err)
		}
		ctx.Variable = ctx.Variable.MinLength(val)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := parseInt64Param(param)
		if err != nil {
			return fmt.Errorf("min parameter must be an integer: %w", err)
		}
		ctx.Variable = ctx.Variable.MinInt(val)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := parseUint64Param(param)
		if err != nil {
			return fmt.Errorf("min parameter must be an unsigned integer: %w", err)
		}
		ctx.Variable = ctx.Variable.MinUint(val)

	case reflect.Float32, reflect.Float64:
		val, err := parseFloat64Param(param)
		if err != nil {
			return fmt.Errorf("min parameter must be a float: %w", err)
		}
		ctx.Variable = ctx.Variable.MinFloat(val)

	default:
		return fmt.Errorf("min directive is not supported for field type %s", fieldType)
	}

	return nil
}

func MaxDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("max needs exactly one parameter")
	}

	fieldType := ctx.FieldValue.Type()
	param := dir.Params[0]

	switch fieldType.Kind() {
	case reflect.String:
		val, err := parseIntParam(param)
		if err != nil {
			return fmt.Errorf("max parameter must be an integer for string length: %w", err)
		}
		ctx.Variable = ctx.Variable.MaxLength(val)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := parseInt64Param(param)
		if err != nil {
			return fmt.Errorf("max parameter must be an integer: %w", err)
		}
		ctx.Variable = ctx.Variable.MaxInt(val)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := parseUint64Param(param)
		if err != nil {
			return fmt.Errorf("max parameter must be an unsigned integer: %w", err)
		}
		ctx.Variable = ctx.Variable.MaxUint(val)

	case reflect.Float32, reflect.Float64:
		val, err := parseFloat64Param(param)
		if err != nil {
			return fmt.Errorf("max parameter must be a float: %w", err)
		}
		ctx.Variable = ctx.Variable.MaxFloat(val)

	default:
		return fmt.Errorf("max directive is not supported for field type %s", fieldType)
	}

	return nil
}

func RangeDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 2 {
		return fmt.Errorf("range needs exactly two parameters")
	}

	fieldType := ctx.FieldValue.Type()
	minParam := dir.Params[0]
	maxParam := dir.Params[1]

	switch fieldType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		mn, err := parseInt64Param(minParam)
		if err != nil {
			return fmt.Errorf("range min parameter must be an integer: %w", err)
		}

		mx, err := parseInt64Param(maxParam)
		if err != nil {
			return fmt.Errorf("range max parameter must be an integer: %w", err)
		}

		ctx.Variable = ctx.Variable.IntRange(mn, mx)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		mn, err := parseUint64Param(minParam)
		if err != nil {
			return fmt.Errorf("range min parameter must be an unsigned integer: %w", err)
		}

		mx, err := parseUint64Param(maxParam)
		if err != nil {
			return fmt.Errorf("range max parameter must be an unsigned integer: %w", err)
		}

		ctx.Variable = ctx.Variable.UintRange(mn, mx)

	case reflect.Float32, reflect.Float64:
		mn, err := parseFloat64Param(minParam)
		if err != nil {
			return fmt.Errorf("range min parameter must be a float: %w", err)
		}

		mx, err := parseFloat64Param(maxParam)
		if err != nil {
			return fmt.Errorf("range max parameter must be a float: %w", err)
		}

		ctx.Variable = ctx.Variable.FloatRange(mn, mx)

	default:
		return fmt.Errorf("range directive is not supported for field type %s", fieldType)
	}

	return nil
}

func RegexpDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) != 1 {
		return fmt.Errorf("regexp needs exactly one parameter")
	}

	expr, err := regexp.Compile(dir.Params[0])
	if err != nil {
		return fmt.Errorf("invalid regexp pattern: %w", err)
	}

	ctx.Variable = ctx.Variable.MatchRegexp(expr)
	return nil
}

func OneOfDirectiveHandler(ctx *FieldContext, dir Directive) error {
	if len(dir.Params) == 0 {
		return fmt.Errorf("oneOf needs at least one parameter")
	}

	ctx.Variable = ctx.Variable.OneOf(dir.Params...)
	return nil
}

type TypeHandler interface {
	HandleType(ctx *FieldContext) (any, error)
}

type KindHandler interface {
	HandleKind(ctx *FieldContext) (any, error)
}

type TimeHandler struct{}

func (h *TimeHandler) HandleType(ctx *FieldContext) (any, error) {
	return ctx.Variable.Time(ctx.TimeLayout)
}

type URLHandler struct{}

func (h *URLHandler) HandleType(ctx *FieldContext) (any, error) {
	return ctx.Variable.URL()
}

type StringHandler struct{}

func (h *StringHandler) HandleKind(ctx *FieldContext) (any, error) {
	return ctx.Variable.String()
}

type BoolHandler struct{}

func (h *BoolHandler) HandleKind(ctx *FieldContext) (any, error) {
	return ctx.Variable.Boolean()
}

type IntHandler struct{}

func (h *IntHandler) HandleKind(ctx *FieldContext) (any, error) {
	value, err := ctx.Variable.Int()
	if err != nil {
		return 0, err
	}

	fieldType := ctx.FieldValue.Type()
	switch fieldType.Kind() {
	case reflect.Int8:
		if value < -128 || value > 127 {
			return 0, fmt.Errorf("value %d overflows int8", value)
		}
		return int8(value), nil
	case reflect.Int16:
		if value < -32768 || value > 32767 {
			return 0, fmt.Errorf("value %d overflows int16", value)
		}
		return int16(value), nil
	case reflect.Int32:
		if value < -2147483648 || value > 2147483647 {
			return 0, fmt.Errorf("value %d overflows int32", value)
		}
		return int32(value), nil
	default:
		return value, nil
	}
}

type Int64Handler struct{}

func (h *Int64Handler) HandleKind(ctx *FieldContext) (any, error) {
	return ctx.Variable.Int64()
}

type UintHandler struct{}

func (h *UintHandler) HandleKind(ctx *FieldContext) (any, error) {
	value, err := ctx.Variable.Uint64()
	if err != nil {
		return 0, err
	}

	fieldType := ctx.FieldValue.Type()
	switch fieldType.Kind() {
	case reflect.Uint:
		if value > uint64(^uint(0)) {
			return 0, fmt.Errorf("value %d overflows uint", value)
		}
		return uint(value), nil
	case reflect.Uint8:
		if value > 255 {
			return 0, fmt.Errorf("value %d overflows uint8", value)
		}
		return uint8(value), nil
	case reflect.Uint16:
		if value > 65535 {
			return 0, fmt.Errorf("value %d overflows uint16", value)
		}
		return uint16(value), nil
	case reflect.Uint32:
		if value > 4294967295 {
			return 0, fmt.Errorf("value %d overflows uint32", value)
		}
		return uint32(value), nil
	default:
		return value, nil
	}
}

type FloatHandler struct{}

func (h *FloatHandler) HandleKind(ctx *FieldContext) (any, error) {
	value, err := ctx.Variable.Float64()
	if err != nil {
		return 0, err
	}

	if ctx.FieldValue.Type().Kind() == reflect.Float32 {
		return float32(value), nil
	}
	return value, nil
}

type SliceHandler struct{}

func (h *SliceHandler) HandleKind(ctx *FieldContext) (any, error) {
	elemType := ctx.FieldValue.Type().Elem()

	switch elemType.Kind() {
	case reflect.String:
		return ctx.Variable.StringSlice(ctx.Delimiter)
	case reflect.Int:
		return ctx.Variable.Each(ctx.Delimiter).IntSlice()
	case reflect.Int64:
		if elemType == reflect.TypeOf(time.Duration(0)) {
			return ctx.Variable.Each(ctx.Delimiter).DurationSlice()
		}
		return ctx.Variable.Each(ctx.Delimiter).Int64Slice()
	case reflect.Uint:
		return ctx.Variable.Each(ctx.Delimiter).UintSlice()
	case reflect.Uint8:
		return ctx.Variable.Each(ctx.Delimiter).Uint8Slice()
	case reflect.Uint16:
		return ctx.Variable.Each(ctx.Delimiter).Uint16Slice()
	case reflect.Uint32:
		return ctx.Variable.Each(ctx.Delimiter).Uint32Slice()
	case reflect.Uint64:
		return ctx.Variable.Each(ctx.Delimiter).Uint64Slice()
	case reflect.Float32:
		return ctx.Variable.Each(ctx.Delimiter).Float32Slice()
	case reflect.Float64:
		return ctx.Variable.Each(ctx.Delimiter).Float64Slice()
	case reflect.Bool:
		return ctx.Variable.Each(ctx.Delimiter).BooleanSlice()
	default:
		return nil, fmt.Errorf("unsupported slice element type %s", elemType)
	}
}

type MapHandler struct{}

func (h *MapHandler) HandleKind(ctx *FieldContext) (any, error) {
	fieldType := ctx.FieldValue.Type()
	if fieldType.Key().Kind() == reflect.String && fieldType.Elem().Kind() == reflect.String {
		return ctx.Variable.MapStringString()
	}
	return nil, fmt.Errorf("unsupported map type %s", fieldType)
}

type StructHandler struct{}

func (h *StructHandler) HandleKind(ctx *FieldContext) (any, error) {
	fieldType := ctx.FieldValue.Type()
	newStruct := reflect.New(fieldType).Interface()
	tag, hasTag := ctx.Field.Tag.Lookup("env")

	var opts []Option
	var nestedPrefix string

	if hasTag && tag != "" {
		tagName := strings.Split(tag, ";")[0]

		if ctx.Prefix != "" && tagName != "" {
			nestedPrefix = ctx.Prefix + tagName + "_"
		} else if tagName != "" {
			nestedPrefix = tagName + "_"
		} else if ctx.Prefix != "" {
			nestedPrefix = ctx.Prefix
		}

		if nestedPrefix != "" {
			opts = append(opts, WithPrefix(nestedPrefix))
		}

		if ctx.EnablePrefixFallback {
			opts = append(opts, WithPrefixFallback(true))
		}
	} else {
		if ctx.Prefix != "" {
			opts = append(opts, WithPrefix(ctx.Prefix))
		}
	}

	if ctx.TagParser != nil {
		opts = append(opts, WithTagParser(ctx.TagParser))
	}

	if ctx.Resolver != nil {
		opts = append(opts, WithResolver(ctx.Resolver))
	}

	err := Load(newStruct, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load nested struct: %w", err)
	}

	return reflect.ValueOf(newStruct).Elem().Interface(), nil
}

type PointerHandler struct{}

func (h *PointerHandler) HandleKind(ctx *FieldContext) (any, error) {
	fieldType := ctx.FieldValue.Type().Elem()
	newValue := reflect.New(fieldType)

	if fieldType.Kind() == reflect.Struct {
		tag, hasTag := ctx.Field.Tag.Lookup("env")

		var opts []Option
		var nestedPrefix string

		if hasTag && tag != "" {
			tagName := strings.Split(tag, ";")[0]

			if ctx.Prefix != "" && tagName != "" {
				nestedPrefix = ctx.Prefix + tagName + "_"
			} else if tagName != "" {
				nestedPrefix = tagName + "_"
			} else if ctx.Prefix != "" {
				nestedPrefix = ctx.Prefix
			}

			if nestedPrefix != "" {
				opts = append(opts, WithPrefix(nestedPrefix))
			}

			if ctx.EnablePrefixFallback {
				opts = append(opts, WithPrefixFallback(true))
			}
		} else {
			if ctx.Prefix != "" {
				opts = append(opts, WithPrefix(ctx.Prefix))

				if ctx.EnablePrefixFallback {
					opts = append(opts, WithPrefixFallback(true))
				}
			}
		}

		if ctx.TagParser != nil {
			opts = append(opts, WithTagParser(ctx.TagParser))
		}

		if ctx.Resolver != nil {
			opts = append(opts, WithResolver(ctx.Resolver))
		}

		err := Load(newValue.Interface(), opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to load struct pointer: %w", err)
		}
	} else {
		return nil, fmt.Errorf("pointers to non-struct types are not supported yet")
	}

	return newValue.Interface(), nil
}

// parseSearchPlan parses a tag into a search plan
func (l *StructLoader) parseSearchPlan(field reflect.StructField, tag string) (SearchPlan, error) {
	parts := strings.Split(tag, ";")
	if len(parts) == 0 {
		return SearchPlan{}, nil
	}

	names := parts[0]
	if names == "" {
		names = toEnvNameFormat(field.Name)
	}
	steps, err := parseSearchSteps(names)
	if err != nil {
		return SearchPlan{}, fmt.Errorf("failed to parse search steps: %w", err)
	}

	var planSteps []SearchStep
	for i, s := range steps {
		if l.prefix != "" && i == 0 && !s.IsQuoted {
			prefixed := SearchStep{
				Name:     l.prefix + s.Name,
				Labels:   s.Labels,
				IsQuoted: false,
			}
			planSteps = append(planSteps, prefixed)
			if l.enablePrefixFallback {
				planSteps = append(planSteps, s)
			}
			continue
		}
		planSteps = append(planSteps, s)
	}

	return SearchPlan{Steps: planSteps}, nil
}

func parseSearchSteps(input string) ([]SearchStep, error) {
	var steps []SearchStep
	start := 0
	n := len(input)
	inQuotes := false
	bracketLevel := 0

	for i := 0; i <= n; i++ {
		isEndOfSegment := (i == n)
		isSeparator := false

		if i < n {
			char := input[i]
			if char == '\'' {
				inQuotes = !inQuotes
			} else if !inQuotes {
				if char == '[' {
					bracketLevel++
				} else if char == ']' {
					bracketLevel--
				} else if char == ',' && bracketLevel == 0 {
					isSeparator = true
				}
			}
		}

		if isSeparator || isEndOfSegment {
			segment := input[start:i]
			trimmedSegment := strings.TrimSpace(segment)

			if len(trimmedSegment) > 0 {
				step, err := parseSingleStep(trimmedSegment)
				if err != nil {
					return nil, fmt.Errorf("error parsing segment '%s' (near index %d): %w", trimmedSegment, start, err)
				}
				steps = append(steps, step)
			} else if len(segment) > 0 && isSeparator {
				// Skip empty segments like from ",,"
			}
			start = i + 1
		}
	}

	if inQuotes {
		return nil, fmt.Errorf("unmatched single quote found at end of input")
	}
	if bracketLevel != 0 {
		if bracketLevel > 0 {
			return nil, fmt.Errorf("mismatched square brackets (extra '[' found)")
		}
		return nil, fmt.Errorf("mismatched square brackets (extra ']' found)")
	}

	return steps, nil
}

func parseSingleStep(segment string) (SearchStep, error) {
	step := SearchStep{Labels: []string{}}

	namePart := segment
	labelPart := ""

	labelOpenIdx := strings.IndexByte(segment, '[')

	if labelOpenIdx != -1 {
		if segment[len(segment)-1] == ']' {
			labelCloseIdx := len(segment) - 1
			if labelOpenIdx < labelCloseIdx {
				namePart = strings.TrimSpace(segment[:labelOpenIdx])
				labelPart = segment[labelOpenIdx+1 : labelCloseIdx]
			} else if labelOpenIdx+1 == labelCloseIdx {
				namePart = strings.TrimSpace(segment[:labelOpenIdx])
				labelPart = ""
			} else {
				return SearchStep{}, fmt.Errorf("invalid bracket placement, '[' not before ']': %s", segment)
			}
		} else {
			return SearchStep{}, fmt.Errorf("found '[' but segment does not end with matching ']': %s", segment)
		}
	}

	if len(namePart) >= 2 && namePart[0] == '\'' && namePart[len(namePart)-1] == '\'' {
		step.Name = namePart[1 : len(namePart)-1]
		step.IsQuoted = true
	} else {
		if strings.ContainsAny(namePart, "'[]") {
			return SearchStep{}, fmt.Errorf("unquoted name '%s' contains invalid characters (' , [ , ])", namePart)
		}
		step.Name = namePart
		step.IsQuoted = false
	}

	if step.Name == "" && !step.IsQuoted {
		return SearchStep{}, fmt.Errorf("parsed step has empty unquoted name in segment: %s", segment)
	}

	if labelPart != "" {
		rawLabels := strings.Split(labelPart, ",")
		for _, lbl := range rawLabels {
			trimmedLabel := strings.TrimSpace(lbl)
			if trimmedLabel != "" {
				if strings.ContainsAny(trimmedLabel, "'[]") {
					return SearchStep{}, fmt.Errorf("label '%s' contains invalid characters (' , [ , ])", trimmedLabel)
				}
				step.Labels = append(step.Labels, trimmedLabel)
			}
		}
	}
	if step.Labels == nil {
		step.Labels = []string{}
	}

	return step, nil
}

type DefaultTagParser struct{}

func (p *DefaultTagParser) formatError(err error) error {
	return fmt.Errorf("invalid search plan: %w", err)
}

func NewTagParser() TagParser {
	return &DefaultTagParser{}
}

func (p *DefaultTagParser) Parse(tag string) (Tag, error) {
	var result Tag
	result.Names = []string{}
	parts := strings.Split(tag, ";")

	if len(parts) > 0 {
		namesList := strings.Split(parts[0], ",")
		for _, name := range namesList {
			name = strings.TrimSpace(name)
			if name != "" {
				// Extract name without labels
				nameOnly := name
				if openBracketIdx := strings.Index(name, "["); openBracketIdx != -1 {
					closeBracketIdx := strings.LastIndex(name, "]")
					if closeBracketIdx == -1 || closeBracketIdx <= openBracketIdx {
						return result, p.formatError(fmt.Errorf("missing closing bracket"))
					}
					if closeBracketIdx < len(name)-1 {
						return result, p.formatError(fmt.Errorf("unexpected characters after closing bracket"))
					}
					nameOnly = strings.TrimSpace(name[:openBracketIdx])
					if nameOnly == "" {
						return result, p.formatError(fmt.Errorf("empty name before labels"))
					}
				}
				if strings.Contains(name, "]") && !strings.Contains(name, "[") {
					return result, p.formatError(fmt.Errorf("invalid label syntax"))
				}
				if nameOnly != "" {
					result.Names = append(result.Names, nameOnly)
				}
			}
		}
	}

	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}

		directive, err := parseDirective(part)
		if err != nil {
			return result, fmt.Errorf("invalid directive %q: %w", part, err)
		}

		result.Directives = append(result.Directives, Directive{
			Name:   directive.name,
			Params: directive.params,
		})
	}

	return result, nil
}

type directiveParser struct {
	name   string
	params []string
}

func parseDirective(directive string) (directiveParser, error) {
	if directive == "" {
		return directiveParser{}, fmt.Errorf("empty directive")
	}

	paramStart := strings.Index(directive, "(")
	if paramStart == -1 {
		return directiveParser{name: directive}, nil
	}

	name := directive[:paramStart]
	paramEnd := strings.LastIndex(directive, ")")
	if paramEnd == -1 || paramEnd <= paramStart {
		return directiveParser{}, fmt.Errorf("invalid directive format: %s", directive)
	}

	paramsStr := directive[paramStart+1 : paramEnd]
	var params []string
	var currentParam strings.Builder
	var escaped bool

	for _, c := range paramsStr {
		if escaped {
			currentParam.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == ',' {
			params = append(params, currentParam.String())
			currentParam.Reset()
			continue
		}
		currentParam.WriteRune(c)
	}
	if currentParam.Len() > 0 {
		params = append(params, currentParam.String())
	}

	return directiveParser{
		name:   name,
		params: params,
	}, nil
}

func toEnvNameFormat(s string) string {
	if s == "" {
		return ""
	}

	var result strings.Builder
	result.WriteRune(unicode.ToUpper(rune(s[0])))

	for i := 1; i < len(s); i++ {
		current := rune(s[i])
		if unicode.IsUpper(current) {
			prev := rune(s[i-1])
			if unicode.IsLower(prev) {
				result.WriteRune('_')
			} else if i+1 < len(s) {
				next := rune(s[i+1])
				if unicode.IsUpper(prev) && unicode.IsLower(next) {
					result.WriteRune('_')
				}
			}
		}
		result.WriteRune(unicode.ToUpper(current))
	}
	return result.String()
}

func parseIntParam(s string) (int, error) {
	return strconv.Atoi(s)
}

func parseInt64Param(s string) (int64, error) {
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return val, nil
}

func parseUint64Param(s string) (uint64, error) {
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return val, nil
}

func parseFloat64Param(s string) (float64, error) {
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return val, nil
}

var CallBoolMethod = callBoolMethod

func callBoolMethod(cfg any, methodName string) (bool, error) {
	method := reflect.ValueOf(cfg).MethodByName(methodName)
	if !method.IsValid() {
		return false, fmt.Errorf("method %s not found", methodName)
	}

	results := method.Call(nil)
	if len(results) != 1 {
		return false, fmt.Errorf("method %s does not return exactly one value", methodName)
	}

	boolResult, ok := results[0].Interface().(bool)
	if !ok {
		return false, fmt.Errorf("method %s does not return a boolean", methodName)
	}

	return boolResult, nil
}

var CallValidateMethod = callValidateMethod

func callValidateMethod(cfg any, methodName string, value any) error {
	method := reflect.ValueOf(cfg).MethodByName(methodName)
	if !method.IsValid() {
		return fmt.Errorf("method %s not found", methodName)
	}

	valueType := reflect.TypeOf(value)
	methodType := method.Type()

	if methodType.NumIn() != 1 {
		return fmt.Errorf("validation method %s must take exactly one argument", methodName)
	}
	if methodType.NumOut() != 1 || !methodType.Out(0).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return fmt.Errorf("validation method %s must return an error", methodName)
	}

	if !valueType.AssignableTo(methodType.In(0)) {
		return fmt.Errorf("validation method %s expects argument of type %s, got %s",
			methodName, methodType.In(0), valueType)
	}

	results := method.Call([]reflect.Value{reflect.ValueOf(value)})
	errInterface := results[0].Interface()
	if errInterface == nil {
		return nil
	}
	return errInterface.(error)
}

var CallConvertMethod = callConvertMethod

func callConvertMethod(cfg any, methodName string, stringValue string, targetType reflect.Type) (any, error) {
	method := reflect.ValueOf(cfg).MethodByName(methodName)
	if !method.IsValid() {
		return nil, fmt.Errorf("conversion method %s not found", methodName)
	}

	methodType := method.Type()

	if methodType.NumIn() != 1 {
		return nil, fmt.Errorf("conversion method %s must take exactly one string argument", methodName)
	}
	if methodType.NumOut() != 2 ||
		!methodType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return nil, fmt.Errorf("conversion method %s must return (value, error)", methodName)
	}

	if methodType.In(0).Kind() != reflect.String {
		return nil, fmt.Errorf("conversion method %s must accept a string parameter", methodName)
	}

	if !methodType.Out(0).AssignableTo(targetType) {
		return nil, fmt.Errorf("conversion method %s returns type %s, but field is of type %s",
			methodName, methodType.Out(0), targetType)
	}

	results := method.Call([]reflect.Value{reflect.ValueOf(stringValue)})

	errInterface := results[1].Interface()
	if errInterface != nil {
		return nil, errInterface.(error)
	}

	return results[0].Interface(), nil
}
