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
