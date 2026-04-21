//go:build api || all

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// historyDBParams holds PostgreSQL connection parameters.
type historyDBParams struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode,omitempty"`
}

func (p *historyDBParams) applyDefaults() {
	if p.Host == "" {
		p.Host = "localhost"
	}
	if p.Port == 0 {
		p.Port = 5432
	}
	if p.User == "" {
		p.User = "postgres"
	}
	if p.DBName == "" {
		p.DBName = "tentacle"
	}
	if p.SSLMode == "" {
		p.SSLMode = "disable"
	}
}

func (p historyDBParams) connString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=3",
		p.Host, p.Port, p.User, p.Password, p.DBName, p.SSLMode,
	)
}

// paramsFromConfigStore reads the saved history.* KV config and returns connection params.
func (m *Module) paramsFromConfigStore() historyDBParams {
	p := historyDBParams{}
	get := func(key string) string {
		if data, _, err := m.bus.KVGet("tentacle_config", "history."+key); err == nil && len(data) > 0 {
			return string(data)
		}
		return ""
	}
	p.Host = get("HISTORY_DB_HOST")
	if v := get("HISTORY_DB_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.Port = n
		}
	}
	p.User = get("HISTORY_DB_USER")
	p.Password = get("HISTORY_DB_PASSWORD")
	p.DBName = get("HISTORY_DB_NAME")
	p.applyDefaults()
	return p
}

// handleHistoryDBStatus reports on the state of the history database:
// reachable, timescaledb extension present, packages installed locally.
// GET /api/v1/history/db-status
func (m *Module) handleHistoryDBStatus(w http.ResponseWriter, r *http.Request) {
	params := m.paramsFromConfigStore()
	status := map[string]interface{}{
		"params":              params,
		"pgBinaryInstalled":   pgInstalledLocally(),
		"timescaleInstalled":  timescaleInstalledLocally(),
		"canInstallLocally":   canInstallLocally(),
		"reachable":           false,
		"extensionCreated":    false,
		"extensionAvailable":  false,
	}

	db, err := sql.Open("postgres", params.connString())
	if err != nil {
		status["error"] = err.Error()
		writeJSON(w, http.StatusOK, status)
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		status["error"] = err.Error()
		writeJSON(w, http.StatusOK, status)
		return
	}
	status["reachable"] = true

	// Check extension availability (package installed at OS level)
	var count int
	_ = db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_available_extensions WHERE name = 'timescaledb'`,
	).Scan(&count)
	status["extensionAvailable"] = count > 0

	// Check extension created on this DB
	_ = db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_extension WHERE extname = 'timescaledb'`,
	).Scan(&count)
	status["extensionCreated"] = count > 0

	writeJSON(w, http.StatusOK, status)
}

// handleHistoryDBTest tests a PostgreSQL connection with the provided parameters.
// POST /api/v1/history/db-test
func (m *Module) handleHistoryDBTest(w http.ResponseWriter, r *http.Request) {
	var params historyDBParams
	if err := readJSON(r, &params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	params.applyDefaults()

	result := map[string]interface{}{
		"success":            false,
		"extensionAvailable": false,
		"extensionCreated":   false,
	}

	db, err := sql.Open("postgres", params.connString())
	if err != nil {
		result["error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		result["error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result["success"] = true

	var count int
	_ = db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_available_extensions WHERE name = 'timescaledb'`,
	).Scan(&count)
	result["extensionAvailable"] = count > 0

	_ = db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_extension WHERE extname = 'timescaledb'`,
	).Scan(&count)
	result["extensionCreated"] = count > 0

	writeJSON(w, http.StatusOK, result)
}

// handleHistoryDBInstall installs PostgreSQL + TimescaleDB locally (apt-based systems)
// and creates a database/user matching the provided parameters. Emits NDJSON progress
// events so the client can show each step going from "running" to "ok"/"failed" live.
// POST /api/v1/history/db-install
func (m *Module) handleHistoryDBInstall(w http.ResponseWriter, r *http.Request) {
	var params historyDBParams
	if err := readJSON(r, &params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	params.applyDefaults()

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	emit := func(event map[string]interface{}) {
		_ = enc.Encode(event)
		if flusher != nil {
			flusher.Flush()
		}
	}
	emitDone := func(success bool, errMsg string) {
		ev := map[string]interface{}{"type": "done", "success": success}
		if errMsg != "" {
			ev["error"] = errMsg
		}
		emit(ev)
	}

	if runtime.GOOS != "linux" {
		emitDone(false, "local install is only supported on Linux (apt-based systems)")
		return
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		emitDone(false, "apt-get not found; local install requires an apt-based distribution")
		return
	}
	if os.Geteuid() != 0 {
		emitDone(false, "local install requires root privileges")
		return
	}

	// stepID lets the frontend match "running" events to later "ok"/"failed" updates
	// without relying on label equality.
	var stepID int
	emitStep := func(id int, label, status, errMsg string) {
		ev := map[string]interface{}{"type": "step", "id": id, "step": label, "status": status}
		if errMsg != "" {
			ev["error"] = errMsg
		}
		emit(ev)
	}
	runStep := func(label string, args ...string) error {
		stepID++
		id := stepID
		emitStep(id, label, "running", "")
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			emitStep(id, label, "failed", msg)
			return fmt.Errorf("%s: %s", label, msg)
		}
		emitStep(id, label, "ok", "")
		return nil
	}
	runPsqlStep := func(label, sqlStmt string) error {
		return runStep(label, "sudo", "-u", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-c", sqlStmt)
	}
	completedStep := func(label string) {
		stepID++
		emitStep(stepID, label, "ok", "")
	}

	// 1. Install PostgreSQL if missing.
	if !pgInstalledLocally() {
		if err := runStep("apt-get update", "apt-get", "update"); err != nil {
			emitDone(false, err.Error())
			return
		}
		if err := runStep("install postgresql", "apt-get", "install", "-y", "postgresql", "postgresql-contrib"); err != nil {
			emitDone(false, err.Error())
			return
		}
	} else {
		completedStep("postgresql already installed")
	}

	// 2. Install TimescaleDB if missing.
	if !timescaleInstalledLocally() {
		if err := runStep("install apt prereqs", "apt-get", "install", "-y", "gnupg", "postgresql-common", "apt-transport-https", "lsb-release", "wget", "ca-certificates"); err != nil {
			emitDone(false, err.Error())
			return
		}
		if err := runStep("add timescale apt repo", "bash", "-c",
			`echo "deb https://packagecloud.io/timescale/timescaledb/ubuntu/ $(lsb_release -c -s) main" > /etc/apt/sources.list.d/timescaledb.list && wget --quiet -O - https://packagecloud.io/timescale/timescaledb/gpgkey | gpg --dearmor -o /etc/apt/trusted.gpg.d/timescaledb.gpg`,
		); err != nil {
			emitDone(false, err.Error())
			return
		}
		if err := runStep("apt-get update (timescale)", "apt-get", "update"); err != nil {
			emitDone(false, err.Error())
			return
		}
		pgMajor, err := detectPostgresMajor()
		if err != nil {
			stepID++
			emitStep(stepID, "detect pg version", "failed", err.Error())
			emitDone(false, "detect pg version: "+err.Error())
			return
		}
		pkg := fmt.Sprintf("timescaledb-2-postgresql-%s", pgMajor)
		if err := runStep("install "+pkg, "apt-get", "install", "-y", pkg); err != nil {
			emitDone(false, err.Error())
			return
		}
		if _, err := exec.LookPath("timescaledb-tune"); err == nil {
			// tuning is a nice-to-have — downgrade failure to warning.
			stepID++
			id := stepID
			emitStep(id, "timescaledb-tune", "running", "")
			cmd := exec.Command("timescaledb-tune", "--quiet", "--yes")
			cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = err.Error()
				}
				emitStep(id, "timescaledb-tune", "warning", msg)
			} else {
				emitStep(id, "timescaledb-tune", "ok", "")
			}
		}
		if err := runStep("restart postgresql", "systemctl", "restart", "postgresql"); err != nil {
			emitDone(false, err.Error())
			return
		}
	} else {
		completedStep("timescaledb already installed")
	}

	// 3. Ensure postgres service is running.
	if err := runStep("start postgresql", "systemctl", "start", "postgresql"); err != nil {
		emitDone(false, err.Error())
		return
	}

	// 4. Configure password for postgres user and create database.
	if err := runPsqlStep("alter postgres password",
		fmt.Sprintf("ALTER USER postgres WITH PASSWORD '%s';", sqlEscape(params.Password))); err != nil {
		emitDone(false, err.Error())
		return
	}
	dbExists, err := dbExistsLocally(params.DBName)
	if err != nil {
		stepID++
		emitStep(stepID, "check database exists", "failed", err.Error())
		emitDone(false, err.Error())
		return
	}
	if !dbExists {
		if err := runPsqlStep("create database",
			fmt.Sprintf(`CREATE DATABASE "%s";`, strings.ReplaceAll(params.DBName, `"`, `""`))); err != nil {
			emitDone(false, err.Error())
			return
		}
	} else {
		completedStep("database already exists")
	}

	// 5. Create timescaledb extension on the target db. Extension creation failure is
	// non-fatal — the module will retry on start.
	stepID++
	id := stepID
	emitStep(id, "create timescaledb extension", "running", "")
	cmd := exec.Command("sudo", "-u", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-d", params.DBName, "-c", `CREATE EXTENSION IF NOT EXISTS timescaledb;`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		emitStep(id, "create timescaledb extension", "warning", msg)
	} else {
		emitStep(id, "create timescaledb extension", "ok", "")
	}

	emitDone(true, "")
}

// pgInstalledLocally checks whether the PostgreSQL server package is installed.
func pgInstalledLocally() bool {
	if _, err := exec.LookPath("psql"); err != nil {
		return false
	}
	// Check for postgres unit file
	if _, err := os.Stat("/usr/lib/systemd/system/postgresql.service"); err == nil {
		return true
	}
	if _, err := os.Stat("/lib/systemd/system/postgresql.service"); err == nil {
		return true
	}
	// Fallback: check for cluster directory
	if _, err := os.Stat("/etc/postgresql"); err == nil {
		return true
	}
	return false
}

// timescaleInstalledLocally checks whether the TimescaleDB package is installed.
func timescaleInstalledLocally() bool {
	cmd := exec.Command("bash", "-c", "ls /usr/lib/postgresql/*/lib/timescaledb*.so 2>/dev/null | head -1")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// canInstallLocally reports whether the process can install PG locally.
func canInstallLocally() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		return false
	}
	return os.Geteuid() == 0
}

// detectPostgresMajor returns the major version of the installed PG server (e.g. "16").
func detectPostgresMajor() (string, error) {
	// Look for /etc/postgresql/<N>/ directories.
	entries, err := os.ReadDir("/etc/postgresql")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				return e.Name(), nil
			}
		}
	}
	return "", fmt.Errorf("no postgres version dir found in /etc/postgresql")
}

// dbExistsLocally checks whether a database exists in the local postgres cluster.
func dbExistsLocally(name string) (bool, error) {
	cmd := exec.Command("sudo", "-u", "postgres", "psql", "-tAc",
		fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s'", sqlEscape(name)))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, err
	}
	return strings.TrimSpace(out.String()) == "1", nil
}

// sqlEscape does minimal single-quote escaping for values interpolated into SQL.
// Only safe for values that are already known to be reasonable (passwords, db names).
func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
