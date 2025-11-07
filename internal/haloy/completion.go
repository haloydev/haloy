package haloy

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewCompletionCmd creates a new completion command
func CompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Long: `To load completions:

Bash:
  # Temporarily (current session only):
  $ source <(haloy completion bash)
  # Permanently:
  $ haloy completion bash | sudo tee /etc/bash_completion.d/haloy > /dev/null  # Linux
  $ haloy completion bash | sudo tee /usr/local/etc/bash_completion.d/haloy > /dev/null  # macOS

Zsh:
  # Create completions directory
  $ mkdir -p ~/.local/share/zsh/site-functions
  $ haloy completion zsh > ~/.local/share/zsh/site-functions/_haloy
  # Add to ~/.zshrc (only needed once):
  $ echo 'fpath=(~/.local/share/zsh/site-functions $fpath)' >> ~/.zshrc
  $ echo 'autoload -U compinit && compinit' >> ~/.zshrc

Fish:
  $ mkdir -p ~/.config/fish/completions
  $ haloy completion fish > ~/.config/fish/completions/haloy.fish

PowerShell:
  PS> haloy completion powershell > haloy.ps1
  # Then source the file from your PowerShell profile
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell type: %s", args[0])
			}
		},
	}

	return cmd
}
