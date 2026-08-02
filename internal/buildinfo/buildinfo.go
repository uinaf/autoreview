package buildinfo

import "fmt"

var (
	version = "dev"
	commit  = "unknown"
)

func Version() string {
	return fmt.Sprintf("autoreview %s (%s)", version, commit)
}
