// Command mpesa-register performs the one-time Daraja registrations.
//
// C2B and Pull are two separate bindings Safaricom holds against the
// shortcode, so they are two subcommands rather than one. Both are expensive to
// get wrong — the URLs are what Safaricom posts real payment notifications to,
// and rebinding them is a support request, not a redeploy — so both print what
// they are about to do and refuse to act without --confirm.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	if err := cfg.Payments.Mpesa.Validate(cfg.Server.ServerEnvironment); err != nil {
		log.Fatalf("M-Pesa config invalid: %v", err)
	}

	command := os.Args[1]

	switch command {
	case "c2b":
		registerC2B(cfg, os.Args[2:])
	case "pull":
		registerPull(cfg, os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

// registerC2B binds the validation and confirmation URLs to the shortcode.
func registerC2B(cfg *config.Config, args []string) {
	flags := flag.NewFlagSet("c2b", flag.ExitOnError)
	confirm := flags.Bool("confirm", false, "actually perform the registration")
	_ = flags.Parse(args)

	mp := cfg.Payments.Mpesa
	validationURL := mp.DarajaCallbackURL("c2b/validation")
	confirmationURL := mp.DarajaCallbackURL("c2b/confirmation")

	fmt.Println("C2B URL registration")
	fmt.Printf("  environment:   %s\n", cfg.Server.ServerEnvironment)
	fmt.Printf("  shortcode:     %d\n", mp.CollectionShortcode)
	fmt.Printf("  response type: %s\n", mpesa.ResponseTypeCompleted)
	fmt.Printf("  validation:    %s\n", validationURL)
	fmt.Printf("  confirmation:  %s\n", confirmationURL)

	if !*confirm {
		fmt.Println("\nNothing was sent. Re-run with --confirm to register.")
		return
	}

	client := newClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	resp, err := client.RegisterURL(ctx, mpesa.RegisterURLRequest{
		Shortcode:       mp.CollectionShortcode,
		ResponseType:    mpesa.ResponseTypeCompleted,
		ValidationURL:   validationURL,
		ConfirmationURL: confirmationURL,
	})
	cancel()
	if err != nil {
		log.Fatalf("C2B registration failed: %v", err)
	}
	fmt.Printf("\nRegistered. ResponseCode=%s ResponseDescription=%s\n",
		resp.ResponseCode, resp.ResponseDescription)
}

// registerPull binds the shortcode to the Pull API.
func registerPull(cfg *config.Config, args []string) {
	flags := flag.NewFlagSet("pull", flag.ExitOnError)
	confirm := flags.Bool("confirm", false, "actually perform the registration")
	nominated := flags.String("nominated-number", "", "the number Safaricom associates with the registration")
	_ = flags.Parse(args)

	mp := cfg.Payments.Mpesa
	callbackURL := mp.DarajaCallbackURL("pull/result")

	fmt.Println("Pull API registration")
	fmt.Printf("  environment:      %s\n", cfg.Server.ServerEnvironment)
	fmt.Printf("  shortcode:        %d\n", mp.CollectionShortcode)
	fmt.Printf("  nominated number: %s\n", *nominated)
	fmt.Printf("  callback:         %s\n", callbackURL)

	if !*confirm {
		fmt.Println("\nNothing was sent. Re-run with --confirm to register.")
		return
	}

	client := newClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	resp, err := client.PullRegister(ctx, mpesa.PullRegisterRequest{
		Shortcode:       mp.CollectionShortcode,
		NominatedNumber: *nominated,
		CallbackURL:     callbackURL,
	})
	cancel()
	if err != nil {
		log.Fatalf("Pull registration failed: %v", err)
	}
	// 1001 means the shortcode was already bound, which is the desired end
	// state rather than a failure.
	if resp.AlreadyRegistered() {
		fmt.Printf("\nAlready registered (%s). Nothing changed.\n", resp.ResponseDescription)
		return
	}
	fmt.Printf("\nRegistered. ResponseStatus=%s ResponseDescription=%s\n",
		resp.ResponseStatus, resp.ResponseDescription)
}

func newClient(cfg *config.Config) *mpesa.Client {
	environment := mpesa.EnvironmentSandbox
	if cfg.Server.ServerEnvironment == "production" {
		environment = mpesa.EnvironmentProduction
	}
	client, err := mpesa.New(mpesa.Config{
		Environment:         environment,
		ConsumerKey:         cfg.Payments.Mpesa.ConsumerKey,
		ConsumerSecret:      cfg.Payments.Mpesa.ConsumerSecret,
		CollectionShortcode: cfg.Payments.Mpesa.CollectionShortcode,
		Passkey:             cfg.Payments.Mpesa.Passkey,
		InitiatorName:       cfg.Payments.Mpesa.InitiatorName,
		InitiatorPassword:   cfg.Payments.Mpesa.InitiatorPassword,
	})
	if err != nil {
		log.Fatalf("M-Pesa client construction failed: %v", err)
	}
	return client
}

func printUsage() {
	fmt.Println("M-Pesa Daraja Registration CLI")
	fmt.Println("\nUsage:")
	fmt.Println("  mpesa-register <command> [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  c2b   Register the C2B validation and confirmation URLs")
	fmt.Println("  pull  Register the shortcode with the Pull API")
	fmt.Println("\nFlags:")
	fmt.Println("  --confirm                  Perform the registration; without it nothing is sent")
	fmt.Println("  --nominated-number <msisdn>  (pull only) number Safaricom associates with the registration")
	fmt.Println("\nExamples:")
	fmt.Println("  mpesa-register c2b")
	fmt.Println("  mpesa-register c2b --confirm")
	fmt.Println("  mpesa-register pull --nominated-number 254700000000 --confirm")
}
