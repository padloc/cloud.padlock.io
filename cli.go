package main

import (
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/customer"
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/yaml.v2"

	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
)

type CliConfig struct {
	Stripe   StripeConfig   `yaml:"stripe"`
	Mixpanel MixpanelConfig `yaml:"mixpanel"`
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

func (cliApp *CliApp) InitWithConfig(config *CliConfig) error {
	cliApp.Config = config
	return nil
}

func (cliApp *CliApp) RunServer(context *cli.Context) error {
	if err := cliApp.InitServer(); err != nil {
		return err
	}

	cliApp.Server = NewServer(cliApp.CliApp.Server, &cliApp.Config.Stripe, &cliApp.Config.Mixpanel)

	if err := cliApp.Server.Init(); err != nil {
		return err
	}

	cfg, _ := yaml.Marshal(cliApp.CliApp.Config)
	cfg2, _ := yaml.Marshal(cliApp.Config)
	cliApp.Server.Info.Printf("Running server with the following configuration:\n%s%s", cfg, cfg2)

	if cliApp.CliApp.Server.Config.Test {
		fmt.Println("*** TEST MODE ***")
	}

	return cliApp.Server.Start()
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

func (cliApp *CliApp) UpdateAccount(context *cli.Context) error {
	email := context.Args().Get(0)
	if email == "" {
		return errors.New("Please provide an email address!")
	}

	cid := context.String("cid")

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

	var err error
	if acc.Customer, err = customer.Get(cid, nil); err != nil {
		return err
	}

	if err := cliApp.Storage.Put(acc); err != nil {
		return err
	}

	return nil
}

func (cliApp *CliApp) DeleteAccount(context *cli.Context) error {
	email := context.Args().Get(0)
	if email == "" {
		return errors.New("Please provide an email address!")
	}
	acc := &Account{Email: email}

	if err := cliApp.Storage.Open(); err != nil {
		return err
	}
	defer cliApp.Storage.Close()

	return cliApp.Storage.Delete(acc)
}

func (cliApp *CliApp) SyncCustomers(context *cli.Context) error {
	tracker := NewMixpanelTracker(cliApp.Config.Mixpanel.Token, cliApp.Storage)

	if err := cliApp.Storage.Open(); err != nil {
		return err
	}
	defer cliApp.Storage.Close()

	params := &stripe.CustomerListParams{}
	// params.Filters.AddFilter("limit", "", "100")
	params.Filters.AddFilter("include[]", "", "total_count")
	i := customer.List(params)

	curr := 0
	count := i.Meta().TotalCount
	nupd := 0
	ndel := 0
	nretries := 0

	fmt.Printf("Processing %d customers...\n", count)

	var iter func() error
	iter = func() error {
		for i.Next() {
			curr = curr + 1
			fmt.Printf("Processing customer %d/%d ... ", curr, count)

			c := i.Customer()
			acc := &Account{Email: c.Email}

			if err := cliApp.Storage.Get(acc); err == nil {

				if acc.Customer == nil || c.ID == acc.Customer.ID {

					fmt.Printf("Found account with matching customer ID: %s; Updating...\n", acc.Email)
					acc.SetCustomer(c)
					if err := cliApp.Storage.Put(acc); err != nil {
						return err
					}
					nupd = nupd + 1

					go tracker.UpdateProfile(acc, nil)

				} else {

					fmt.Printf("Found account with different customer ID: %s; Deleting stripe customer...\n", acc.Email)

					if c.DefaultSource != nil {
						fmt.Println("Customer has payment source! Bailing...")
					} else {
						ndel = ndel + 1
						go customer.Del(c.ID, nil)
					}

				}
			} else if err == pc.ErrNotFound {

				fmt.Printf("Account not found: %s; Deleting stripe customer...\n", acc.Email)

				if c.DefaultSource != nil {
					fmt.Println("Customer has payment source! Bailing...")
				} else {
					ndel = ndel + 1
					go customer.Del(c.ID, nil)
				}

			} else {
				return err
			}
		}

		if err := i.Err(); err != nil && nretries < 100 {
			nretries = nretries + 1
			fmt.Printf("Encountered error %v - retrying (%d/100)\n", err, nretries)
			return iter()
		}

		return nil
	}

	if err := iter(); err != nil {
		return err
	}

	fmt.Printf("Customers Updated: %d\nCustomers Deleted: %d\n", nupd, ndel)

	return nil
}

func NewCliApp() *CliApp {
	config := &CliConfig{}
	pcCli := pc.NewCliApp()
	app := &CliApp{
		CliApp: pcCli,
	}

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
		cli.StringFlag{
			Name:        "mixpanel-token",
			Value:       "",
			Usage:       "Mixpanel token",
			EnvVar:      "PC_MIXPANEL_TOKEN",
			Destination: &config.Mixpanel.Token,
		},
	}...)

	runserverCmd := &app.Commands[0]
	runserverCmd.Action = app.RunServer

	app.Commands = append(app.Commands, []cli.Command{
		{
			Name:  "sub",
			Usage: "Commands for managing subscriptions",
			Subcommands: []cli.Command{
				{
					Name:   "display",
					Usage:  "Display a given subscription account",
					Action: app.DisplayAccount,
				},
				{
					Name:   "update",
					Usage:  "Update a given account",
					Action: app.UpdateAccount,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  "cid",
							Value: "",
							Usage: "Stripe customer id",
						},
					},
				},
				{
					Name:   "delete",
					Usage:  "Delete account",
					Action: app.DeleteAccount,
				},
				{
					Name:   "sync",
					Usage:  "Sync Stripe Customers",
					Action: app.SyncCustomers,
				},
			},
		},
	}...)

	before := app.Before
	app.Before = func(context *cli.Context) error {
		before(context)

		if app.ConfigPath != "" {
			// Replace original config object to prevent flags from being applied
			config = &CliConfig{}
			if err := config.LoadFromFile(app.ConfigPath); err != nil {
				return err
			}
		}

		stripe.Key = config.Stripe.SecretKey

		if err := app.InitWithConfig(config); err != nil {
			fmt.Println(err)
			return err
		}

		return nil
	}

	return app
}
