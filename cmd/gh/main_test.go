package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func Test_printError(t *testing.T) {
	cmd := &cobra.Command{}

	type args struct {
		err   error
		cmd   *cobra.Command
		debug bool
	}
	tests := []struct {
		name    string
		args    args
		wantOut string
	}{
		{
			name: "generic error",
			args: args{
				err:   errors.New("the app exploded"),
				cmd:   nil,
				debug: false,
			},
			wantOut: "the app exploded\n",
		},
		{
			name: "DNS error",
			args: args{
				err: fmt.Errorf("DNS oopsie: %w", &net.DNSError{
					Name: "api.github.com",
				}),
				cmd:   nil,
				debug: false,
			},
			wantOut: `error connecting to api.github.com
check your internet connection or https://githubstatus.com
`,
		},
		{
			name: "Cobra flag error",
			args: args{
				err:   cmdutil.FlagErrorf("unknown flag --foo"),
				cmd:   cmd,
				debug: false,
			},
			wantOut: "unknown flag --foo\n\nUsage:\n\n",
		},
		{
			name: "unknown Cobra command error",
			args: args{
				err:   errors.New("unknown command foo"),
				cmd:   cmd,
				debug: false,
			},
			wantOut: "unknown command foo\n\nUsage:\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			printError(out, tt.args.err, tt.args.cmd, tt.args.debug)
			if gotOut := out.String(); gotOut != tt.wantOut {
				t.Errorf("printError() = %q, want %q", gotOut, tt.wantOut)
			}
		})
	}
}

func Test_repoCommand(t *testing.T) {
	cmd := &cobra.Command{
		Use: "repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("repository name is required")
			}
			repoName := args[0]
			client, err := cmdutil.NewFactory().HttpClient()
			if err != nil {
				return err
			}
			repo, err := api.FetchRepository(client, repoName)
			if err != nil {
				return err
			}
			fmt.Printf("Repository: %s\nDescription: %s\n", repo.Name, repo.Description)
			return nil
		},
	}

	tests := []struct {
		name    string
		args    []string
		wantOut string
		wantErr bool
	}{
		{
			name:    "no args",
			args:    []string{},
			wantOut: "",
			wantErr: true,
		},
		{
			name:    "valid repo",
			args:    []string{"cli/cli"},
			wantOut: "Repository: cli/cli\nDescription: GitHub’s official command line tool\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("repoCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotOut := out.String(); gotOut != tt.wantOut {
				t.Errorf("repoCommand() = %q, want %q", gotOut, tt.wantOut)
			}
		})
	}
}

func Test_userCommand(t *testing.T) {
	cmd := &cobra.Command{
		Use: "user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("username is required")
			}
			username := args[0]
			client, err := cmdutil.NewFactory().HttpClient()
			if err != nil {
				return err
			}
			user, err := api.GetUser(client, username)
			if err != nil {
				return err
			}
			fmt.Printf("User: %s\nName: %s\n", user.Login, user.Name)
			return nil
		},
	}

	tests := []struct {
		name    string
		args    []string
		wantOut string
		wantErr bool
	}{
		{
			name:    "no args",
			args:    []string{},
			wantOut: "",
			wantErr: true,
		},
		{
			name:    "valid user",
			args:    []string{"octocat"},
			wantOut: "User: octocat\nName: The Octocat\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("userCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotOut := out.String(); gotOut != tt.wantOut {
				t.Errorf("userCommand() = %q, want %q", gotOut, tt.wantOut)
			}
		})
	}
}
