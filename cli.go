package main

import (
	"errors"
	"fmt"
	"io/ioutil"

	"gopkg.in/urfave/cli.v1"
	"gopkg.in/yaml.v2"

	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
)

type CliConfig struct {
	Stripe StripeConfig `yaml:"stripe"`
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
	Config *CliConfig
}

func (cliApp *CliApp) InitConfig() {
	cliApp.Config = &CliConfig{}
	cliApp.Server.StripeConfig = &cliApp.Config.Stripe
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

// func (cliApp *CliApp) CreatePlan(context *cli.Context) error {
// 	var (
// 		email      string
// 		subType    string
// 		expiresStr string
// 	)
//
// 	if email = context.String("account"); email == "" {
// 		return errors.New("Please provide an email address!")
// 	}
// 	if expiresStr = context.String("expires"); expiresStr == "" {
// 		return errors.New("Please provide an expiration date!")
// 	}
// 	if subType = context.String("type"); subType == "" {
// 		return errors.New("Please provide a subscription type!")
// 	}
//
// 	expires, err := time.Parse("2006/01/02", expiresStr)
// 	if err != nil {
// 		return errors.New("Failed to parse expiration date!")
// 	}
//
// 	acc := &Account{
// 		Email: email,
// 	}
//
// 	switch subType {
// 	case "free":
// 		acc.Plans.Free = &FreePlan{
// 			&Plan{
// 				Expires: expires,
// 			},
// 		}
// 	default:
// 		return errors.New("Invalid subscription type")
// 	}
//
// 	if err := cliApp.Storage.Open(); err != nil {
// 		return err
// 	}
// 	defer cliApp.Storage.Close()
//
// 	if err := cliApp.Storage.Put(acc); err != nil {
// 		return err
// 	}
//
// 	return nil
// }

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
	server := NewServer(pcCli.Server)
	app := &CliApp{
		pcCli,
		server,
		nil,
	}
	app.InitConfig()
	config := app.Config

	app.Flags = append(app.Flags, []cli.Flag{
		cli.StringFlag{
			Name:        "stripe-secret-key",
			Value:       "",
			Usage:       "Stripe secret key",
			EnvVar:      "PC_STRIPE_SECRET_KEY",
			Destination: &config.Stripe.SecretKey,
		},
		cli.StringFlag{
			Name:        "stripe-public-key",
			Value:       "",
			Usage:       "Stripe public key",
			EnvVar:      "PC_STRIPE_PUBLIC_KEY",
			Destination: &config.Stripe.PublicKey,
		},
	}...)

	runserverCmd := &app.Commands[0]
	runserverCmd.Action = app.RunServer

	app.Commands = append(app.Commands, []cli.Command{
		{
			Name:  "sub",
			Usage: "Commands for managing subscriptions",
			Subcommands: []cli.Command{
				// {
				// 	Name:   "update",
				// 	Usage:  "Create subscription for a given account",
				// 	Action: app.CreatePlan,
				// 	Flags: []cli.Flag{
				// 		cli.StringFlag{
				// 			Name:  "account",
				// 			Value: "",
				// 			Usage: "Email address of the account to create the subscription for",
				// 		},
				// 		cli.StringFlag{
				// 			Name:  "type",
				// 			Value: "free",
				// 			Usage: "Plan type; Currently only 'free' is supported (default)",
				// 		},
				// 		cli.StringFlag{
				// 			Name:  "expires",
				// 			Value: "",
				// 			Usage: "Expiration date; Must be in the form 'YYYY/MM/DD'",
				// 		},
				// 	},
				// },
				{
					Name:   "display",
					Usage:  "Display a given subscription account",
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
