// sbx/internal/naming/naming.go
package naming

import "fmt"

func Short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func EnvName(session string, seq int) string {
	return fmt.Sprintf("sbx-%s-%03d", Short(session, 8), seq)
}

// Network is the per-namespace bridge network name. Isolating each env on its
// own network is what keeps N parallel envs from seeing each other.
func Network(namespace string) string { return namespace + "-net" }

// Volume namespaces a named volume under an env so parallel envs never share
// state through a volume of the same logical name.
func Volume(namespace, name string) string { return namespace + "-" + name }
