package main

import "io/ioutil"
import "errors"
import "time"
import "fmt"

import "gopkg.in/yaml.v2"
import "gopkg.in/urfave/cli.v1"

import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

type CliConfig struct {
	Itunes ItunesConfig `yaml:"itunes"`
}

func (c *CliConfig) LoadFromFile(path string) error {
	// load config file
	yamlData, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlData, c)
	if err != nil {
		return err
	}

	return nil
}

type CliApp struct {
	*pc.CliApp
	Server *Server
	Itunes *ItunesServer
	Config *CliConfig
}

func (cliApp *CliApp) InitConfig() {
	cliApp.Config = &CliConfig{}
	cliApp.Itunes.Config = &cliApp.Config.Itunes
}

func (cliApp *CliApp) RunServer(context *cli.Context) error {
	cfg, _ := yaml.Marshal(cliApp.CliApp.Config)
	cfg2, _ := yaml.Marshal(cliApp.Config)
	cliApp.Server.Info.Printf("Running server with the following configuration:\n%s%s", cfg, cfg2)

	if err := cliApp.Server.Init(); err != nil {
		return err
	}

	return cliApp.Server.Start()
}

func (cliApp *CliApp) CreatePlan(context *cli.Context) error {
	var (
		email      string
		subType    string
		expiresStr string
	)

	if email = context.String("account"); email == "" {
		return errors.New("Please provide an email address!")
	}
	if expiresStr = context.String("expires"); expiresStr == "" {
		return errors.New("Please provide an expiration date!")
	}
	if subType = context.String("type"); subType == "" {
		return errors.New("Please provide a plan type!")
	}

	expires, err := time.Parse("2006/01/02", expiresStr)
	if err != nil {
		return errors.New("Failed to parse expiration date!")
	}

	acc := &Account{
		Email: email,
	}

	switch subType {
	case "free":
		acc.Plans.Free = &FreePlan{
			&Plan{
				Expires: expires,
			},
		}
	default:
		return errors.New("Invalid plan type")
	}

	if err := cliApp.Storage.Open(); err != nil {
		return err
	}
	defer cliApp.Storage.Close()

	if err := cliApp.Storage.Put(acc); err != nil {
		return err
	}

	return nil
}

func (cliApp *CliApp) DisplayAccount(context *cli.Context) error {
	email := context.Args().Get(0)
	if email == "" {
		return errors.New("Please provide an email address!")
	}

	if err := cliApp.Storage.Open(); err != nil {
		return err
	}
	defer cliApp.Storage.Close()

	acc := &Account{
		Email: email,
	}

	if err := cliApp.Storage.Get(acc); err != nil {
		return err
	}

	yamlData, err := yaml.Marshal(acc)
	if err != nil {
		return err
	}

	fmt.Println(string(yamlData))

	return nil
}

func NewCliApp() *CliApp {
	pcCli := pc.NewCliApp()
	itunes := &ItunesServer{}
	server := NewServer(pcCli.Server, itunes)
	app := &CliApp{
		pcCli,
		server,
		itunes,
		nil,
	}
	app.InitConfig()
	config := app.Config

	app.Flags = append(app.Flags, []cli.Flag{
		cli.StringFlag{
			Name:        "itunes-shared-secret",
			Usage:       "'Shared Secret' used for authenticating with itunes",
			Value:       "",
			EnvVar:      "PC_ITUNES_SHARED_SECRET",
			Destination: &config.Itunes.SharedSecret,
		},
		cli.StringFlag{
			Name:        "itunes-environment",
			Usage:       "Determines which itunes server to send requests to. Can be 'sandbox' (default) or 'production'.",
			Value:       "sandbox",
			EnvVar:      "PC_ITUNES_ENVIRONMENT",
			Destination: &config.Itunes.Environment,
		},
	}...)

	runserverCmd := &app.Commands[0]
	runserverCmd.Action = app.RunServer

	app.Commands = append(app.Commands, []cli.Command{
		{
			Name:  "plans",
			Usage: "Commands for managing plans",
			Subcommands: []cli.Command{
				{
					Name:   "update",
					Usage:  "Create plan for a given account",
					Action: app.CreatePlan,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  "account",
							Value: "",
							Usage: "Email address of the account to create the plan for",
						},
						cli.StringFlag{
							Name:  "type",
							Value: "free",
							Usage: "Plan type; Currently only 'free' is supported (default)",
						},
						cli.StringFlag{
							Name:  "expires",
							Value: "",
							Usage: "Expiration date; Must be in the form 'YYYY/MM/DD'",
						},
					},
				},
				{
					Name:   "displayaccount",
					Usage:  "Display a given plan account",
					Action: app.DisplayAccount,
				},
			},
		},
	}...)

	before := app.Before
	app.Before = func(context *cli.Context) error {
		before(context)

		if app.ConfigPath != "" {
			// Replace original config object to prevent flags from being applied
			app.InitConfig()
			return app.Config.LoadFromFile(app.ConfigPath)
		}
		return nil
	}

	return app
}
