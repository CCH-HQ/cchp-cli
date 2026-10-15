package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	cchpcli "github.com/CCH-HQ/cchp-cli"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

type guideTopic struct {
	Name    string `json:"name"`
	File    string `json:"-"`
	Summary string `json:"summary"`
}

var guideTopics = []guideTopic{
	{"getting-started", "getting-started.md", "Sign in and run your first commands."},
	{"tokens", "tokens.md", "Management tokens, permissions and why a call is refused."},
	{"tasks", "tasks.md", "Everyday tasks: usage, providers, keys and recent requests."},
	{"errors", "errors.md", "What each error means and what to do next."},
	{"agents", "agents.md", "How agents and scripts should call cchp."},
}

func (a *app) guideCommand() *cobra.Command {
	names := make([]string, len(guideTopics))
	for i, t := range guideTopics {
		names[i] = t.Name
	}
	return &cobra.Command{
		Use:       "guide [topic]",
		Short:     "Short guides: " + strings.Join(names, ", "),
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: names,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if a.agent {
					return a.writeData(map[string]any{"topics": guideTopics})
				}
				w := ui.Writer(a.io.Out)
				fmt.Fprintln(w, ui.Title.Render("Guides"))
				for _, t := range guideTopics {
					fmt.Fprintf(w, "  %-16s %s\n", ui.Key.Render(t.Name), t.Summary)
				}
				fmt.Fprintln(w, ui.Muted.Render("Open one with: cchp guide <topic>"))
				return nil
			}
			for _, t := range guideTopics {
				if t.Name != args[0] {
					continue
				}
				text, err := fs.ReadFile(cchpcli.Docs, "guides/"+t.File)
				if err != nil {
					return err
				}
				if a.agent {
					return a.writeData(map[string]any{"topic": t.Name, "markdown": string(text)})
				}
				return a.renderMarkdown(string(text))
			}
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "there is no guide %q", args[0]).WithHint("Topics: %s", strings.Join(names, ", "))
		},
	}
}

func (a *app) renderMarkdown(text string) error {
	style := "notty"
	if a.io.OutTTY && os.Getenv("NO_COLOR") == "" {
		style = "auto"
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(88))
	if err != nil {
		return err
	}
	out, err := r.Render(text)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(a.io.Out, out)
	return err
}

func (a *app) skillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install or print the cchp agent skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	var agentsMD bool
	var file string
	show := &cobra.Command{
		Use:   "show",
		Short: "Print the skill (SKILL.md), one of its references, or an AGENTS.md snippet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "skills/cchp/SKILL.md"
			switch {
			case agentsMD:
				name = "skills/agents-md-snippet.md"
			case file != "":
				name = path.Join("skills/cchp", path.Clean("/" + file)[1:])
			}
			text, err := fs.ReadFile(cchpcli.Docs, name)
			if err != nil {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "the skill has no file %q", file).WithHint("Files: %s", strings.Join(skillFiles(), ", "))
			}
			if a.agent && a.jsonOut {
				return a.writeData(map[string]any{"path": strings.TrimPrefix(name, "skills/"), "markdown": string(text)})
			}
			_, err = a.io.Out.Write(text)
			return err
		},
	}
	show.Flags().BoolVar(&agentsMD, "agents-md", false, "Print a snippet to paste into a project's AGENTS.md.")
	show.Flags().StringVar(&file, "file", "", "Print one file of the skill, for example references/auth.md.")

	var project, user, force bool
	var dir string
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the skill for Claude Code (project: .claude/skills/cchp, user: ~/.claude/skills/cchp)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := dir
			switch {
			case target != "":
			case project && user:
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "choose --project or --user, not both")
			case user:
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				target = filepath.Join(home, ".claude", "skills", "cchp")
			default:
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				target = filepath.Join(wd, ".claude", "skills", "cchp")
			}
			written, err := installSkill(target, force)
			if err != nil {
				return err
			}
			if a.agent {
				return a.writeData(map[string]any{"directory": target, "files": written})
			}
			fmt.Fprintln(ui.Writer(a.io.Out), ui.Success.Render("✓ ")+fmt.Sprintf("Installed the cchp skill (%d files) in %s", len(written), target))
			return nil
		},
	}
	install.Flags().BoolVar(&project, "project", false, "Install into this project's .claude/skills/cchp (the default).")
	install.Flags().BoolVar(&user, "user", false, "Install into ~/.claude/skills/cchp for every project.")
	install.Flags().StringVar(&dir, "dir", "", "Install into this directory instead.")
	install.Flags().BoolVar(&force, "force", false, "Replace files that differ from this version of cchp.")
	cmd.AddCommand(show, install)
	return cmd
}

func skillFiles() []string {
	var out []string
	err := fs.WalkDir(cchpcli.Docs, "skills/cchp", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, strings.TrimPrefix(p, "skills/cchp/"))
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Strings(out)
	return out
}

// installSkill copies the embedded skill into target. Files that already hold
// the same content are left alone; different files need --force.
func installSkill(target string, force bool) ([]string, error) {
	var conflicts []string
	files := skillFiles()
	for _, rel := range files {
		want, err := fs.ReadFile(cchpcli.Docs, "skills/cchp/"+rel)
		if err != nil {
			return nil, err
		}
		have, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return nil, err
		case !bytes.Equal(have, want) && !force:
			conflicts = append(conflicts, rel)
		}
	}
	if len(conflicts) > 0 {
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "these files in %s differ from this cchp version: %s", target, strings.Join(conflicts, ", ")).
			WithHint("Add --force to replace them.")
	}
	for _, rel := range files {
		data, err := fs.ReadFile(cchpcli.Docs, "skills/cchp/"+rel)
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, err
		}
	}
	return files, nil
}
