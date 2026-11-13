package conduitcli

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

//go:embed conduit-profile-v1.schema.json
var conduitProfileSchema []byte

type connectionProfile struct {
	SpaceID         string `json:"spaceId,omitempty" yaml:"spaceId,omitempty"`
	Name            string `json:"name" yaml:"name"`
	Server          string `json:"server" yaml:"server"`
	Timeout         string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Output          string `json:"output,omitempty" yaml:"output,omitempty"`
	CAFile          string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	ClientCertFile  string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	ClientKeyFile   string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`
	SPIFFEEndpoint  string `json:"spiffeEndpoint,omitempty" yaml:"spiffeEndpoint,omitempty"`
	HubSPIFFEID     string `json:"hubSpiffeId,omitempty" yaml:"hubSpiffeId,omitempty"`
	BearerTokenFile string `json:"bearerTokenFile,omitempty" yaml:"bearerTokenFile,omitempty"`
}

type profileDocument struct {
	Version  int                          `json:"version" yaml:"version"`
	Current  string                       `json:"current,omitempty" yaml:"current,omitempty"`
	Profiles map[string]connectionProfile `json:"profiles" yaml:"profiles"`
}

func (profile connectionProfile) validate(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("profile name is required")
	}
	if profile.Name != name {
		return fmt.Errorf("profile %q name must match its map key", name)
	}
	timeout := defaultTimeout
	if profile.Timeout != "" {
		parsed, err := time.ParseDuration(profile.Timeout)
		if err != nil {
			return fmt.Errorf("profile %q timeout: %w", name, err)
		}
		timeout = parsed
	}
	if profile.Output != "" {
		switch strings.ToLower(strings.TrimSpace(profile.Output)) {
		case "json", "yaml":
		default:
			return fmt.Errorf("profile %q output must be json or yaml", name)
		}
	}
	configuration := ConnectionConfig{
		Server: profile.Server, Timeout: timeout,
		CAFile: profile.CAFile, ClientCertFile: profile.ClientCertFile, ClientKeyFile: profile.ClientKeyFile,
		SPIFFEEndpoint: profile.SPIFFEEndpoint, HubSPIFFEID: profile.HubSPIFFEID,
		BearerTokenFile: profile.BearerTokenFile, SpaceID: profile.SpaceID,
	}
	if _, err := configuration.validate(); err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	return nil
}

func validateProfileDocument(document profileDocument) error {
	if document.Version != 1 {
		return fmt.Errorf("unsupported Conduit profile document version %d", document.Version)
	}
	if document.Current != "" {
		if _, exists := document.Profiles[document.Current]; !exists {
			return fmt.Errorf("current Conduit profile %q does not exist", document.Current)
		}
	}
	for name, profile := range document.Profiles {
		if err := profile.validate(name); err != nil {
			return err
		}
	}
	return nil
}

func (state *commandState) profilePath() (string, error) {
	if configured := strings.TrimSpace(state.runtime.Getenv("CONDUIT_CONFIG_FILE")); configured != "" {
		return configured, nil
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(directory, "conduit", "config.yaml"), nil
}

func (state *commandState) readProfiles() (profileDocument, error) {
	path, err := state.profilePath()
	if err != nil {
		return profileDocument{}, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return profileDocument{Version: 1, Profiles: map[string]connectionProfile{}}, nil
	}
	if err != nil {
		return profileDocument{}, fmt.Errorf("read Conduit profiles: %w", err)
	}
	var document profileDocument
	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return profileDocument{}, fmt.Errorf("decode Conduit profiles: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return profileDocument{}, errors.New("Conduit profile file must contain exactly one YAML document")
	}
	if document.Profiles == nil {
		document.Profiles = map[string]connectionProfile{}
	}
	if err := validateProfileDocument(document); err != nil {
		return profileDocument{}, err
	}
	return document, nil
}

func (state *commandState) writeProfiles(document profileDocument) error {
	if document.Profiles == nil {
		document.Profiles = map[string]connectionProfile{}
	}
	if err := validateProfileDocument(document); err != nil {
		return err
	}
	path, err := state.profilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Conduit config directory: %w", err)
	}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode Conduit profiles: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("create Conduit config transaction: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect Conduit config transaction: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write Conduit config transaction: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync Conduit config transaction: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Conduit config transaction: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("commit Conduit config transaction: %w", err)
	}
	return nil
}

func (state *commandState) currentProfile(name string) (connectionProfile, error) {
	document, err := state.readProfiles()
	if err != nil {
		return connectionProfile{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = document.Current
	}
	profile, exists := document.Profiles[name]
	if !exists {
		return connectionProfile{}, fmt.Errorf("Conduit profile %q does not exist", name)
	}
	return profile, nil
}

func (state *commandState) applySelectedProfile(command *cobra.Command) error {
	if state.profile == "" {
		document, err := state.readProfiles()
		if err != nil {
			return err
		}
		if document.Current == "" {
			return nil
		}
		state.profile = document.Current
	}
	profile, err := state.currentProfile(state.profile)
	if err != nil {
		return err
	}
	if state.config.SpaceID == "" && !command.Flags().Changed("space-id") {
		state.config.SpaceID = profile.SpaceID
	}
	if state.config.Server == "" {
		state.config.Server = profile.Server
	}
	if state.config.CAFile == "" {
		state.config.CAFile = profile.CAFile
	}
	if state.config.ClientCertFile == "" {
		state.config.ClientCertFile = profile.ClientCertFile
	}
	if state.config.ClientKeyFile == "" {
		state.config.ClientKeyFile = profile.ClientKeyFile
	}
	if state.config.SPIFFEEndpoint == "" {
		state.config.SPIFFEEndpoint = profile.SPIFFEEndpoint
	}
	if state.config.HubSPIFFEID == "" {
		state.config.HubSPIFFEID = profile.HubSPIFFEID
	}
	if state.config.BearerTokenFile == "" {
		state.config.BearerTokenFile = profile.BearerTokenFile
	}
	if !command.Flags().Changed("output") && profile.Output != "" {
		state.output = profile.Output
	}
	if !command.Flags().Changed("timeout") && profile.Timeout != "" {
		timeout, err := time.ParseDuration(profile.Timeout)
		if err != nil {
			return fmt.Errorf("profile %q timeout: %w", state.profile, err)
		}
		state.config.Timeout = timeout
	}
	return nil
}

func (state *commandState) profileFromFlags(name string) (connectionProfile, error) {
	profile := connectionProfile{
		Name: name, Server: state.config.Server, Timeout: state.config.Timeout.String(), Output: state.output,
		CAFile: state.config.CAFile, ClientCertFile: state.config.ClientCertFile, ClientKeyFile: state.config.ClientKeyFile,
		SPIFFEEndpoint: state.config.SPIFFEEndpoint, HubSPIFFEID: state.config.HubSPIFFEID,
		BearerTokenFile: state.config.BearerTokenFile, SpaceID: state.config.SpaceID,
	}
	configuration := state.config
	configuration.DevelopmentSubject = ""
	configuration.DevelopmentGroups = nil
	if _, err := configuration.validate(); err != nil {
		return connectionProfile{}, err
	}
	return profile, nil
}

func configureCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Create or update a secure local connection profile",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			name := state.profile
			if name == "" {
				name = "default"
			}
			profile, err := state.profileFromFlags(name)
			if err != nil {
				return err
			}
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			document.Profiles[name] = profile
			document.Current = name
			if err := state.writeProfiles(document); err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, profile)
		},
	}
}

func profileCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "profile", Short: "Manage local connection profiles"}
	command.AddCommand(&cobra.Command{
		Use: "schema", Short: "Print the versioned profile document JSON Schema", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := state.runtime.Stdout.Write(conduitProfileSchema); err != nil {
				return fmt.Errorf("write Conduit profile schema: %w", err)
			}
			_, err := state.runtime.Stdout.Write([]byte("\n"))
			return err
		},
	})
	command.AddCommand(profileCreateCommand(state))
	command.AddCommand(&cobra.Command{
		Use: "list", Short: "List profiles", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(document.Profiles))
			for name := range document.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"current": document.Current, "profiles": names})
		},
	})
	command.AddCommand(profileShowCommand(state, "show"))
	command.AddCommand(profileShowCommand(state, "export"))
	command.AddCommand(profileDeleteCommand(state))
	command.AddCommand(profileRenameCommand(state))
	command.AddCommand(profileUseCommand(state))
	command.AddCommand(profileImportCommand(state))
	return command
}

func profileCreateCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use: "create NAME", Short: "Create a profile from the connection flags", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			if _, exists := document.Profiles[args[0]]; exists {
				return fmt.Errorf("profile %q already exists", args[0])
			}
			profile, err := state.profileFromFlags(args[0])
			if err != nil {
				return err
			}
			document.Profiles[args[0]] = profile
			if document.Current == "" {
				document.Current = args[0]
			}
			if err := state.writeProfiles(document); err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, profile)
		},
	}
}

func profileShowCommand(state *commandState, action string) *cobra.Command {
	return &cobra.Command{
		Use: action + " [NAME]", Short: strings.ToUpper(action[:1]) + action[1:] + " a profile", Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			profile, err := state.currentProfile(name)
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, profile)
		},
	}
}

func profileDeleteCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use: "delete NAME", Short: "Delete a local profile", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			if _, exists := document.Profiles[args[0]]; !exists {
				return fmt.Errorf("profile %q does not exist", args[0])
			}
			delete(document.Profiles, args[0])
			if document.Current == args[0] {
				document.Current = ""
			}
			return state.writeProfiles(document)
		},
	}
}

func profileRenameCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use: "rename OLD NEW", Short: "Rename a local profile", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			profile, exists := document.Profiles[args[0]]
			if !exists {
				return fmt.Errorf("profile %q does not exist", args[0])
			}
			if _, exists := document.Profiles[args[1]]; exists {
				return fmt.Errorf("profile %q already exists", args[1])
			}
			delete(document.Profiles, args[0])
			profile.Name = args[1]
			document.Profiles[args[1]] = profile
			if document.Current == args[0] {
				document.Current = args[1]
			}
			return state.writeProfiles(document)
		},
	}
}

func profileUseCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use: "use NAME", Short: "Select the default profile", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			if _, exists := document.Profiles[args[0]]; !exists {
				return fmt.Errorf("profile %q does not exist", args[0])
			}
			document.Current = args[0]
			return state.writeProfiles(document)
		},
	}
}

func profileImportCommand(state *commandState) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use: "import", Short: "Import a profile document", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			var profile connectionProfile
			if err := decodeDocument(file, &profile); err != nil {
				return err
			}
			if profile.Name == "" {
				return errors.New("imported profile name is required")
			}
			if err := profile.validate(profile.Name); err != nil {
				return err
			}
			document, err := state.readProfiles()
			if err != nil {
				return err
			}
			if _, exists := document.Profiles[profile.Name]; exists {
				return fmt.Errorf("profile %q already exists", profile.Name)
			}
			document.Profiles[profile.Name] = profile
			return state.writeProfiles(document)
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "profile YAML or JSON file")
	_ = command.MarkFlagRequired("file")
	return command
}
