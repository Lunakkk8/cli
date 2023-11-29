package authswitch

import (
	"errors"
	"fmt"
	"slices"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/pkg/cmd/auth/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

type SwitchOptions struct {
	IO       *iostreams.IOStreams
	Config   func() (config.Config, error)
	Prompter shared.Prompt
	Hostname string
	User     string
}

func NewCmdSwitch(f *cmdutil.Factory, runF func(*SwitchOptions) error) *cobra.Command {
	opts := SwitchOptions{
		IO:       f.IOStreams,
		Config:   f.Config,
		Prompter: f.Prompter,
	}

	cmd := &cobra.Command{
		Use:     "switch",
		Args:    cobra.ExactArgs(0),
		Short:   "Switch to another GitHub account",
		Long:    heredoc.Doc(""),
		Example: heredoc.Doc(""),
		RunE: func(c *cobra.Command, args []string) error {
			if runF != nil {
				return runF(&opts)
			}

			return switchRun(&opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Hostname, "hostname", "h", "", "The hostname of the GitHub instance to switch account on")
	cmd.Flags().StringVarP(&opts.User, "user", "u", "", "The user to switch to")

	return cmd
}

type hostUser struct {
	host   string
	user   string
	active bool
}

type candidates []hostUser

func (c candidates) inactiveOptions() []hostUser {
	var inactive []hostUser
	for _, candidate := range c {
		if !candidate.active {
			inactive = append(inactive, candidate)
		}
	}
	return inactive
}

func switchRun(opts *SwitchOptions) error {
	hostname := opts.Hostname
	username := opts.User

	cfg, err := opts.Config()
	if err != nil {
		return err
	}
	authCfg := cfg.Authentication()

	knownHosts := authCfg.Hosts()
	if len(knownHosts) == 0 {
		return fmt.Errorf("not logged in to any hosts")
	}

	if hostname != "" {
		if !slices.Contains(knownHosts, hostname) {
			return fmt.Errorf("not logged in to %s", hostname)
		}

		if username != "" {
			knownUsers, _ := cfg.Authentication().UsersForHost(hostname)
			if !slices.Contains(knownUsers, username) {
				return fmt.Errorf("not logged in as %s on %s", username, hostname)
			}
		}
	}

	var candidates candidates

	for _, host := range knownHosts {
		if hostname != "" && host != hostname {
			continue
		}
		hostActiveUser, err := authCfg.User(host)
		if err != nil {
			return err
		}
		knownUsers, err := cfg.Authentication().UsersForHost(host)
		if err != nil {
			return err
		}
		for _, user := range knownUsers {
			if username != "" && user != username {
				continue
			}
			candidates = append(candidates, hostUser{host: host, user: user, active: user == hostActiveUser})
		}
	}

	inactiveCandidates := candidates.inactiveOptions()
	if len(candidates) == 0 {
		return errors.New("no user accounts matched that criteria")
	} else if len(candidates) == 1 {
		hostname = candidates[0].host
		username = candidates[0].user
	} else if len(inactiveCandidates) == 1 {
		hostname = inactiveCandidates[0].host
		username = inactiveCandidates[0].user
	} else if !opts.IO.CanPrompt() {
		return errors.New("unable to determine which user account to switch to, please specify `--hostname` and `--user`")
	} else {
		prompts := make([]string, len(candidates))
		for i, c := range candidates {
			prompt := fmt.Sprintf("%s (%s)", c.user, c.host)
			if c.active {
				prompt += " - active"
			}
			prompts[i] = prompt
		}
		selected, err := opts.Prompter.Select(
			"What account do you want to switch to?", "", prompts)
		if err != nil {
			return fmt.Errorf("could not prompt: %w", err)
		}
		hostname = candidates[selected].host
		username = candidates[selected].user
	}

	if src, writeable := shared.AuthTokenWriteable(authCfg, hostname); !writeable {
		fmt.Fprintf(opts.IO.ErrOut, "The value of the %s environment variable is being used for authentication.\n", src)
		fmt.Fprint(opts.IO.ErrOut, "To have GitHub CLI manage credentials instead, first clear the value from the environment.\n")
		return cmdutil.SilentError
	}

	err = authCfg.SwitchUser(hostname, username)
	if err != nil {
		return err
	}

	cs := opts.IO.ColorScheme()
	fmt.Fprintf(opts.IO.ErrOut, "%s Switched active account on %s to '%s'\n",
		cs.SuccessIcon(), hostname, cs.Bold(username))

	return nil
}
