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
	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sigs
		cancel()
		log.Info().Str("signal", s.String()).Msg("Received signal, exiting")
	}()

	// fetch secrets from AWS Secrets Manager
	secretsToEnvFile()

	res, err := extensionClient.Register(ctx, extensionName)
	if err != nil {
		panic(err)
	}
	log.Info().Interface("reponse", res).Msg("Register response")

	// Will block until shutdown event is received or cancelled via the context.
	processEvents(ctx)
}

func secretsToEnvFile() {
	if _, err := os.Stat(envFile); err == nil {
		log.Info().Msg("Found env file, skipping..")
		return
	}

	f, err := os.OpenFile(envFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	for _, env := range os.Environ() {
		envName := strings.Split(env, "=")[0]
		envValue := strings.Split(env, "=")[1]

		if strings.HasSuffix(envName, "_SECRET_ARN") {
			log.Info().Str("env", envName).Msg("Found secret")
			secretsMap, err := GetSecret(envValue)
			if err != nil {
				panic(err)
			}
			for k, v := range secretsMap {
				log.Info().Str("env", k).Msg("Writing to env file")
				_, err = f.WriteString(fmt.Sprintf("%s=%s\n", k, v))
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
		panic(err.Error())
	}

	var secretString string
	if result.SecretString != nil {
		secretString = *result.SecretString
	}
	secretsMap := make(map[string]string)
	err = json.Unmarshal([]byte(secretString), &secretsMap)
	if err != nil {
		log.Warn().Str("secret", secretName).Msg("Failed to unmarshal secret")
		return make(map[string]string), nil
	}

	return secretsMap, nil
}

func processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Info().Msg("Waiting for event...")
			res, err := extensionClient.NextEvent(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to get next event")
				return
			}
			log.Info().Interface("event", res).Msg("Received event")
			// Exit if we receive a SHUTDOWN event
			if res.EventType == extension.Shutdown {
				log.Info().Msg("Received shutdown event, exiting")
				return
			}
		}
	}
}
