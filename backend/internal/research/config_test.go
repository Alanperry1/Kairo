package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestEnvFileLoadsValuesWithoutOverridingShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv("RESEARCHOS_TEST_EXISTING", "from-shell")
	t.Setenv("RESEARCHOS_TEST_EMPTY", "")
	t.Setenv("RESEARCHOS_TEST_FILE_ONLY", "")
	if err := os.Unsetenv("RESEARCHOS_TEST_FILE_ONLY"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Local values\nRESEARCHOS_TEST_EXISTING=from-file\nRESEARCHOS_TEST_EMPTY=from-file\nRESEARCHOS_TEST_FILE_ONLY=\"quoted value\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("RESEARCHOS_TEST_EXISTING") != "from-shell" {
		t.Fatal("file overrode shell value")
	}
	if os.Getenv("RESEARCHOS_TEST_EMPTY") != "" {
		t.Fatal("file overrode an explicitly empty shell value")
	}
	if os.Getenv("RESEARCHOS_TEST_FILE_ONLY") != "quoted value" {
		t.Fatal("file value was not loaded")
	}
}
func TestEnvFileMissingIsOptional(t *testing.T) {
	if err := loadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatal(err)
	}
}
func TestEnvFileSyntaxErrorDoesNotExposeValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("INVALID!KEY=private-example-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := loadEnvFile(path)
	if err == nil {
		t.Fatal("malformed dotenv file accepted")
	}
	if strings.Contains(err.Error(), "private-example-value") {
		t.Fatal("configuration value leaked into error")
	}
}

func TestFindEnvFileFromRootAndBackend(t *testing.T) {
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	if err := os.Mkdir(backend, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, ".env")
	if err := os.WriteFile(file, []byte("MODEL_PROVIDER=disabled\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// An obsolete backend-local file must not override the shared root file.
	if err := os.WriteFile(filepath.Join(backend, ".env"), []byte("MODEL_PROVIDER=ollama\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{root, backend} {
		got, err := findEnvFile(cwd)
		if err != nil || got != file {
			t.Fatalf("cwd=%s got=%s err=%v", cwd, got, err)
		}
	}
}
func TestFindEnvFileIgnoresLegacyOverrides(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("VITE_API_BASE_URL=/api/v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := findEnvFile(root)
	if err != nil || got != "" {
		t.Fatalf("unexpected dotenv selection: %s %v", got, err)
	}
}

func TestDatabaseURLValidation(t *testing.T) {
	for _, value := range []string{"postgres://user:secret@localhost:5432/kairo?sslmode=disable", "postgresql://localhost/kairo"} {
		if err := validateDatabaseURL(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"", "./data/database", "https://localhost/kairo", "postgres://localhost/", "postgres://user:secret@localhost/%zz"} {
		err := validateDatabaseURL(value)
		if err == nil {
			t.Fatal("invalid database URL accepted")
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("database credentials exposed")
		}
	}
}

func TestNeonConnectionOptions(t *testing.T) {
	value := "postgresql://user:secret@ep-example.us-east-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	if err := validateDatabaseURL(value); err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(value)
	if err != nil {
		t.Fatal("Neon connection string was rejected")
	}
	if config.TLSConfig == nil || config.ChannelBinding != "require" {
		t.Fatal("Neon connection security settings were not retained")
	}
	if _, exists := config.RuntimeParams["channel_binding"]; exists {
		t.Fatal("channel_binding must not be forwarded as a database parameter")
	}
}
