package httpapi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var postgresMajorPattern = regexp.MustCompile(`(\d+)`)

func parsePostgresMajor(version string) (int, error) {
	match := postgresMajorPattern.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) != 2 {
		return 0, fmt.Errorf("versión PostgreSQL no reconocida: %q", version)
	}
	major, err := strconv.Atoi(match[1])
	if err != nil || major < 10 {
		return 0, fmt.Errorf("versión PostgreSQL no compatible: %q", version)
	}
	return major, nil
}

// postgresToolPath prefers the versioned PostgreSQL client shipped in Docker.
// Native installs can use the matching pg_dump/pg_restore from PATH. Requiring
// an exact major keeps backups restorable by the same server major and avoids
// pg_dump refusing a newer server.
func postgresToolPath(name string, serverMajor int) (string, error) {
	if name != "pg_dump" && name != "pg_restore" {
		return "", fmt.Errorf("herramienta PostgreSQL no permitida: %s", name)
	}
	versioned := filepath.Join("/usr/lib/postgresql", strconv.Itoa(serverMajor), "bin", name)
	if info, err := os.Stat(versioned); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
		return versioned, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s no está instalado; se necesita PostgreSQL %d", name, serverMajor)
	}
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("no se pudo comprobar %s: %s", name, strings.TrimSpace(string(output)))
	}
	clientMajor, err := parsePostgresMajor(string(output))
	if err != nil {
		return "", err
	}
	if clientMajor != serverMajor {
		return "", fmt.Errorf("%s es PostgreSQL %d y el servidor usa PostgreSQL %d", name, clientMajor, serverMajor)
	}
	return path, nil
}
