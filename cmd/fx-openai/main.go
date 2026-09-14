package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fx-openai/internal/config"
	"fx-openai/internal/gateway"
	"fx-openai/internal/openai"
	"fx-openai/internal/version"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("fx-openai: ")

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "service":
			if err := runService(args[1:]); err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		case "fx":
			if err := runFX(args[1:]); err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		}
	}

	opts, err := parseArgs(args, os.Stderr)
	if err == flag.ErrHelp {
		return
	}
	if err != nil {
		log.Print(err)
		os.Exit(2)
	}
	if opts.version {
		fmt.Println(version.String)
		return
	}
	if opts.howto {
		fmt.Print(howtoText)
		return
	}
	if opts.printEnv {
		fmt.Print(fxEnvBlock(opts))
		return
	}
	if err := runServer(opts); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

type options struct {
	listen            string
	upstream          string
	apiKey            string
	model             string
	configPath        string
	configAPIKey      string
	configAPIKeyPlain bool
	configBaseURL     string
	configBaseURLFile bool
	printEnv          bool
	howto             bool
	version           bool
}

func parseArgs(args []string, errOut io.Writer) (options, error) {
	configPath, err := configPathForArgs(args)
	if err != nil {
		return options{}, err
	}
	fileCfg, err := config.Load(configPath)
	if err != nil {
		return options{}, err
	}
	upstream, configBaseURLFile, err := fileBaseURL(configPath, fileCfg)
	if err != nil {
		return options{}, err
	}
	apiKey, configAPIKeyPlain, err := fileAPIKey(configPath, fileCfg)
	if err != nil {
		return options{}, err
	}

	opts := options{
		listen:            envOrValue("LISTEN", fileCfg.Listen, "127.0.0.1:8787"),
		upstream:          envOrValue("OPENAI_BASE_URL", upstream, "https://ollama.com/v1"),
		apiKey:            apiKeyFromEnv(apiKey),
		model:             envOrValue("FX_MODEL", fileCfg.Model, ""),
		configPath:        configPath,
		configAPIKey:      apiKey,
		configAPIKeyPlain: configAPIKeyPlain,
		configBaseURL:     upstream,
		configBaseURLFile: configBaseURLFile,
	}

	fs := flag.NewFlagSet("fx-openai", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, "fx-openai %s\n\n", version.String)
		fmt.Fprintln(errOut, "Loopback translator so fx can talk to an OpenAI-compatible server.")
		fmt.Fprintln(errOut, "\nUsage:")
		fmt.Fprintln(errOut, "  fx-openai [flags]                  start the translator")
		fmt.Fprintln(errOut, "  fx-openai fx [-- FX_ARGS...]       run fx with Gateway env injected")
		fmt.Fprintln(errOut, "  fx-openai service <action>         manage the user systemd service")
		fmt.Fprintln(errOut, "\nService actions:")
		fmt.Fprintln(errOut, "  init, start, stop, status, restart, remove")
		fmt.Fprintln(errOut, "\nConfiguration priority: defaults < config.toml < environment < flags.")
		fs.PrintDefaults()
		fmt.Fprintf(errOut, "\nConfig default: %s\nLegacy: fx-openai -print-env\n", configPath)
	}
	fs.StringVar(&opts.listen, "listen", opts.listen, "loopback listen address")
	fs.StringVar(&opts.upstream, "upstream", opts.upstream, "OpenAI-compatible base URL (include /v1)")
	fs.StringVar(&opts.apiKey, "api-key", opts.apiKey, "upstream API key")
	fs.StringVar(&opts.model, "model", opts.model, "model id for fx")
	fs.StringVar(&opts.configPath, "config", opts.configPath, "TOML config path")
	fs.BoolVar(&opts.printEnv, "print-env", false, "print fx env vars and exit")
	fs.BoolVar(&opts.howto, "howto", false, "print setup instructions and exit")
	fs.BoolVar(&opts.version, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func runServer(opts options) error {
	if err := gateway.RequireLoopback(opts.listen); err != nil {
		return err
	}
	key := opts.apiKey
	if key == "" {
		key = "ollama"
		log.Print("no OPENAI_API_KEY / OLLAMA_API_KEY; using dummy key \"ollama\" (fine for local Ollama)")
	}

	srv := &http.Server{
		Addr:              opts.listen,
		Handler:           gateway.New(openai.Config{BaseURL: opts.upstream, APIKey: key}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("%s  http://%s  →  %s", version.String, opts.listen, strings.TrimRight(opts.upstream, "/"))
	log.Print("point fx at this process:")
	for _, line := range strings.Split(strings.TrimSuffix(fxEnvBlock(opts), "\n"), "\n") {
		log.Print("  " + line)
	}
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runFX(args []string) error {
	optionArgs, fxArgs := splitFXArgs(args)
	opts, err := parseArgs(optionArgs, os.Stderr)
	if err != nil {
		return err
	}
	cmd := exec.Command("fx", fxArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withGatewayEnv(os.Environ(), opts)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("fx exited with %s", exitErr.ProcessState)
		}
		return fmt.Errorf("run fx: %w", err)
	}
	return nil
}

func runService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fx-openai service {init|start|stop|status|restart|remove}")
	}
	action := args[0]
	opts, err := parseArgs(args[1:], os.Stderr)
	if err != nil {
		return err
	}
	unitPath := userUnitPath()

	switch action {
	case "init":
		if err := initService(opts, unitPath); err != nil {
			return err
		}
		printServiceLocations(opts, unitPath)
		return nil
	case "start":
		if err := initService(opts, unitPath); err != nil {
			return err
		}
		return systemctlUser("enable", "--now", "fx-openai.service")
	case "stop":
		return systemctlUser("stop", "fx-openai.service")
	case "status":
		return systemctlUser("status", "--no-pager", "fx-openai.service")
	case "restart":
		if err := initService(opts, unitPath); err != nil {
			return err
		}
		return systemctlUser("restart", "fx-openai.service")
	case "remove":
		_ = systemctlUser("disable", "--now", "fx-openai.service")
		if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return systemctlUser("daemon-reload")
	default:
		return fmt.Errorf("unknown service action %q; use init, start, stop, status, restart, or remove", action)
	}
}

func initService(opts options, unitPath string) error {
	if err := ensureConfig(opts); err != nil {
		return err
	}
	if err := encryptConfigAPIKey(opts); err != nil {
		return err
	}
	if err := writeUnit(unitPath, opts.configPath); err != nil {
		return err
	}
	return systemctlUser("daemon-reload")
}

func printServiceLocations(opts options, unitPath string) {
	fmt.Println("fx-openai service initialized")
	fmt.Println("  config.toml:", opts.configPath)
	fmt.Println("  systemd unit:", unitPath)
}

func ensureConfig(opts options) error {
	if _, err := os.Stat(opts.configPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.configPath), 0700); err != nil {
		return err
	}
	contents := "# fx-openai configuration\n" +
		"listen = " + quoteTOML(opts.listen) + "\n" +
		"base_url = " + quoteTOML(opts.upstream) + "\n" +
		"api_key = " + quoteTOML(opts.apiKey) + "\n" +
		"model = " + quoteTOML(opts.model) + "\n"
	return os.WriteFile(opts.configPath, []byte(contents), 0600)
}

func encryptConfigAPIKey(opts options) error {
	if opts.configBaseURLFile && opts.configBaseURL != "" {
		if err := config.UpdateValue(opts.configPath, "base_url", opts.configBaseURL); err != nil {
			return err
		}
	}
	if opts.configAPIKeyPlain && opts.configAPIKey != "" {
		encoded, err := config.EncodeAPIKey(opts.configAPIKey)
		if err != nil {
			return err
		}
		if err := config.UpdateAPIKey(opts.configPath, encoded); err != nil {
			return err
		}
	}
	if opts.configBaseURLFile || opts.configAPIKeyPlain {
		return config.RemoveKeys(opts.configPath, "base_url_file", "api_key_file")
	}
	return nil
}

func writeUnit(path, configPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	unit := "[Unit]\n" +
		"Description=fx-openai loopback OpenAI-compatible translator\n" +
		"After=network-online.target\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"ExecStart=" + exe + " -config " + configPath + "\n" +
		"Restart=on-failure\n" +
		"RestartSec=2s\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
	return os.WriteFile(path, []byte(unit), 0644)
}

func systemctlUser(args ...string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl is required: %w", err)
	}
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fxEnvBlock(opts options) string {
	base := "http://" + opts.listen
	lines := []string{
		"export FX_GATEWAY_BASE_URL=" + base,
		"export FX_GATEWAY_CHAT_URL=" + base + "/v3/ai/language-model",
		"export AI_GATEWAY_API_KEY=local",
		"unset VERCEL_OIDC_TOKEN",
	}
	if opts.model != "" {
		lines = append(lines, "export FX_MODEL="+shellSingleQuote(opts.model))
	} else {
		lines = append(lines, "# model: set model in config.toml, FX_MODEL, -model, or fx's /model command")
	}
	return strings.Join(lines, "\n") + "\n"
}

func withGatewayEnv(env []string, opts options) []string {
	values := map[string]string{
		"FX_GATEWAY_BASE_URL": "http://" + opts.listen,
		"FX_GATEWAY_CHAT_URL": "http://" + opts.listen + "/v3/ai/language-model",
	}
	env = unsetEnv(env, "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN")
	env = append(env, "AI_GATEWAY_API_KEY=local")
	if opts.model != "" {
		values["FX_MODEL"] = opts.model
	}
	for key, value := range values {
		env = setEnv(env, key, value)
	}
	return env
}

func unsetEnv(env []string, keys ...string) []string {
	remove := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		remove[key] = struct{}{}
	}
	kept := env[:0]
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, found := remove[key]; found {
				continue
			}
		}
		kept = append(kept, item)
	}
	return kept
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func splitFXArgs(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return nil, args
}

func configPathForArgs(args []string) (string, error) {
	path, err := defaultConfigPath()
	if err != nil {
		return "", err
	}
	if envPath := os.Getenv("FX_OPENAI_CONFIG"); envPath != "" {
		path = envPath
	}
	for i, arg := range args {
		if arg == "-config" || arg == "--config" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a path", arg)
			}
			path = args[i+1]
		} else if strings.HasPrefix(arg, "-config=") {
			path = strings.TrimPrefix(arg, "-config=")
		} else if strings.HasPrefix(arg, "--config=") {
			path = strings.TrimPrefix(arg, "--config=")
		}
	}
	return path, nil
}

func defaultConfigPath() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "fx-openai", "config.toml"), nil
}

func fileBaseURL(path string, cfg config.File) (value string, fromFile bool, err error) {
	if cfg.BaseURL != "" {
		return cfg.BaseURL, false, nil
	}
	if cfg.BaseURLFile == "" {
		return "", false, nil
	}
	value, err = config.ReadValue(config.ResolvePath(path, cfg.BaseURLFile))
	return value, true, err
}

func fileAPIKey(path string, cfg config.File) (value string, plain bool, err error) {
	raw := cfg.APIKey
	if raw == "" && cfg.APIKeyFile != "" {
		raw, err = config.ReadValue(config.ResolvePath(path, cfg.APIKeyFile))
		if err != nil {
			return "", false, err
		}
	}
	if raw == "" {
		return "", false, nil
	}
	value, encrypted, err := config.DecodeAPIKey(raw)
	return value, !encrypted, err
}

func apiKeyFromEnv(fileValue string) string {
	if value := os.Getenv("OPENAI_API_KEY"); value != "" {
		return value
	}
	if value := os.Getenv("OLLAMA_API_KEY"); value != "" {
		return value
	}
	return fileValue
}

func envOrValue(name, fileValue, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	if fileValue != "" {
		return fileValue
	}
	return fallback
}

func userUnitPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, _ := os.UserHomeDir()
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", "fx-openai.service")
}

func quoteTOML(value string) string {
	return fmt.Sprintf("%q", value)
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

const howtoText = "fx-openai: fx (Vercel Gateway protocol) → this process on loopback → OpenAI /v1.\n\n" +
	"Configuration priority is defaults < config.toml < environment < flags.\n\n" +
	"Examples:\n" +
	"  fx-openai service init\n" +
	"  fx-openai service start\n" +
	"  fx-openai fx -- ask --no-save -- \"Reply with PONG.\"\n" +
	"  eval \"$(fx-openai -print-env)\"\n\n" +
	"The fx subcommand runs the installed fx binary with the loopback Gateway variables\n" +
	"and configured FX_MODEL injected into its environment. It does not modify the\n" +
	"parent shell, which a child process cannot do.\n\n" +
	"The upstream base URL must include /v1 and expose /models and /chat/completions.\n"
