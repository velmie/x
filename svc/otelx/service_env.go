package otelx

import "github.com/velmie/x/svc/confload"

// ServiceEnvAliases maps configuration paths to library-owned OTELX_* names.
// section is the subtree, such as "telemetry"; empty means the root.
func ServiceEnvAliases(section string) map[string]string {
	fields, err := confload.DescribeStruct[ServiceConfig]()
	if err != nil {
		panic(err)
	}
	aliases := fields.EnvMap("OTELX_")
	if section == "" {
		return aliases
	}
	prefixed := make(map[string]string, len(aliases))
	for path, name := range aliases {
		prefixed[section+"."+path] = name
	}
	return prefixed
}
