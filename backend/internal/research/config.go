package research

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	ElevenLabsAPIKey, ElevenLabsAgentID                                                                 string
	Addr, DataDir, Token, ModelProvider, ModelURL, ModelName, ModelKey, Python, WorkerMode, WorkerImage string
	EnvFile                                                                                             string
	DatabaseURL                                                                                         string
	DatabaseURLUnpooled                                                                                 string
	Origins                                                                                             []string
	Workers                                                                                             int
}

func LoadConfig() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	envFile, err := findEnvFile(cwd)
	if err != nil {
		return Config{}, err
	}
	if envFile != "" {
		if err := loadEnvFile(envFile); err != nil {
			return Config{}, err
		}
	}
	c := Config{Addr: env("ADDR", "127.0.0.1:8080"), DataDir: env("DATA_DIR", "./data"), Token: os.Getenv("API_TOKEN"), ModelProvider: env("MODEL_PROVIDER", "ollama"), ModelURL: env("MODEL_URL", "http://127.0.0.1:11434"), ModelName: env("MODEL_NAME", "qwen3:8b"), ModelKey: os.Getenv("MODEL_API_KEY"), Python: env("PYTHON_BIN", "python3"), WorkerMode: env("WORKER_MODE", "local"), WorkerImage: env("WORKER_IMAGE", "researchos-worker:local"), Workers: 1}
	c.EnvFile = envFile
	c.ElevenLabsAPIKey = os.Getenv("ELEVENLABS_API_KEY")
	c.ElevenLabsAgentID = os.Getenv("ELEVENLABS_AGENT_ID")
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.DatabaseURLUnpooled = env("DATABASE_URL_UNPOOLED", c.DatabaseURL)
	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		return c, err
	}
	if err := validateDatabaseURL(c.DatabaseURLUnpooled); err != nil {
		return c, fmt.Errorf("DATABASE_URL_UNPOOLED must be a PostgreSQL connection URL")
	}
	c.Workers, err = strconv.Atoi(env("WORKERS", "1"))
	if err != nil || c.Workers < 1 || c.Workers > 4 {
		return c, fmt.Errorf("WORKERS must be from 1 to 4")
	}
	baseDir := cwd
	if envFile != "" {
		baseDir = filepath.Dir(envFile)
	}
	if !filepath.IsAbs(c.DataDir) {
		c.DataDir = filepath.Join(baseDir, c.DataDir)
	}
	if !filepath.IsAbs(c.Python) && strings.ContainsAny(c.Python, "/\\") {
		c.Python = filepath.Join(baseDir, c.Python)
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	if err != nil {
		return c, err
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return c, fmt.Errorf("ADDR must be host:port")
	}
	ip := net.ParseIP(host)
	if (host != "localhost" && (ip == nil || !ip.IsLoopback())) && len(c.Token) < 24 {
		return c, fmt.Errorf("non-loopback listening requires API_TOKEN with at least 24 characters")
	}
	if c.Token != "" && len(c.Token) < 24 {
		return c, fmt.Errorf("API_TOKEN must contain at least 24 characters")
	}
	if c.ModelProvider != "ollama" && c.ModelProvider != "compatible" && c.ModelProvider != "disabled" {
		return c, fmt.Errorf("MODEL_PROVIDER must be ollama, compatible, or disabled")
	}
	if c.ModelProvider != "disabled" {
		u, err := url.Parse(c.ModelURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("MODEL_URL must be an HTTP(S) base URL without credentials or query parameters")
		}
	}
	if c.WorkerMode != "local" && c.WorkerMode != "docker" {
		return c, fmt.Errorf("WORKER_MODE must be local or docker")
	}
	for _, origin := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
				return c, fmt.Errorf("CORS_ORIGINS must contain exact HTTP(S) origins")
			}
			c.Origins = append(c.Origins, origin)
		}
	}
	return c, nil
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Shell variables, including explicitly empty values, override the local file.
// A missing file is valid for deployments configured entirely by environment.
func loadEnvFile(path string) error {
	if err := godotenv.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not load %s; check file permissions and dotenv syntax", path)
	}
	return nil
}

// Both services use the single project-root .env. The server can be
// started from either the root or backend/ without loading a second file.
func findEnvFile(cwd string) (string, error) {
	root := filepath.Clean(cwd)
	if filepath.Base(root) == "backend" {
		root = filepath.Dir(root)
	}
	path := filepath.Join(root, ".env")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot access shared env file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("shared env path is a directory: %s", path)
	}
	return path, nil
}

func validateDatabaseURL(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("DATABASE_URL is required; paste your remote PostgreSQL connection string into the root .env")
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Path == "" || u.Path == "/" || u.Fragment != "" {
		return fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with a host and database name")
	}
	return nil
}
