package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/rs/zerolog/log"

	"github.com/readly/lambda-extension-aws-secrets-to-env/extension"
)

var (
	extensionName   = filepath.Base(os.Args[0]) // extension name has to match the filename
	extensionClient = extension.NewClient(os.Getenv("AWS_LAMBDA_RUNTIME_API"))
)

const envFile = "/tmp/.env"

func main() {
	log.Logger = log.With().Str("extension_name", extensionName).Logger()
	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		cancel()
	}()

	// fetch secrets from AWS Secrets Manager
	secretsToEnvFile()

	_, err := extensionClient.Register(ctx, extensionName)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to register extension")
	}

	// Will block until shutdown event is received or cancelled via the context.
	processEvents(ctx)
}

func secretsToEnvFile() {
	if _, err := os.Stat(envFile); err == nil {
		err := os.Remove(envFile)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to remove file: %s", envFile)
		}
	}

	f, err := os.OpenFile(envFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to open env file: %s", envFile)
	}
	defer f.Close()

	for _, env := range os.Environ() {
		envName := strings.Split(env, "=")[0]
		envValue := strings.Split(env, "=")[1]

		if strings.HasSuffix(envName, "_SECRET_ARN") {
			secretsMap, err := GetSecret(envValue)
			if err != nil {
				log.Fatal().Err(err).Msg("Failed to get secret")
			}
			for k, v := range secretsMap {
				_, _ = f.WriteString(fmt.Sprintf("%s=%s\n", k, v))
			}
		}
	}
}

func GetSecret(secretName string) (map[string]string, error) {
	svc := secretsmanager.New(
		session.New(),
		aws.NewConfig(),
	)

	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get secret")
	}

	secretsMap := make(map[string]string)
	var secretString string

	if result.SecretString != nil {
		secretString = *result.SecretString
	}

	err = json.Unmarshal([]byte(secretString), &secretsMap)
	if err != nil {
		log.Warn().Str("secret", secretName).Msg("failed to unmarshal secret")
	}

	return secretsMap, nil
}

func processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			res, err := extensionClient.NextEvent(ctx)
			if err != nil {
				log.Error().Err(err).Msg("failed to get next event")
				return
			}
			// Exit if we receive a SHUTDOWN event
			if res.EventType == extension.Shutdown {
				return
			}
		}
	}
}
