package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/readly/lambda-extension-aws-secrets-to-env/extension"
)

var (
	extensionName   = filepath.Base(os.Args[0]) // extension name has to match the filename
	extensionClient = extension.NewClient(os.Getenv("AWS_LAMBDA_RUNTIME_API"))
	// Exclude the following env variables
	extensionExclude = map[string]struct{}{
		"DD_API_KEY_SECRET_ARN": {},
	}
)

const envFile = "/tmp/.env"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)
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
		slog.Error(err.Error())
	}

	// Will block until shutdown event is received or cancelled via the context.
	processEvents(ctx)
}

func secretsToEnvFile() {
	if _, err := os.Stat(envFile); err == nil {
		err := os.Remove(envFile)
		if err != nil {
			slog.Error("failed to remove environment file", "file", envFile, "err", err)
			os.Exit(1)
		}
	}

	f, err := os.OpenFile(envFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Error("failed to open env file", "file", envFile, "err", err)
		os.Exit(1)
	}
	defer f.Close()

	for _, env := range os.Environ() {
		envName := strings.Split(env, "=")[0]
		envValue := strings.Split(env, "=")[1]

		if _, ok := extensionExclude[envName]; !ok && strings.HasSuffix(envName, "_SECRET_ARN") {
			secretsMap, err := GetSecret(envValue)
			if err != nil {
				slog.Error("failed to get secret", "err", err)
			}
			for k, v := range secretsMap {
				_, _ = f.WriteString(fmt.Sprintf("%s=%s\n", k, v))
			}
		}
	}
}

func GetSecret(secretName string) (map[string]string, error) {
	sess := session.Must(session.NewSession(aws.NewConfig()))
	svc := secretsmanager.New(sess)

	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		return nil, err
	}

	secretsMap := make(map[string]string)
	var secretString string

	if result.SecretString != nil {
		secretString = *result.SecretString
	}

	err = json.Unmarshal([]byte(secretString), &secretsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal secret '%s': %w", secretString, err)
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
				slog.Error("failed to fetch next lambda event", "err", err)
				return
			}
			// Exit if we receive a SHUTDOWN event
			if res.EventType == extension.Shutdown {
				return
			}
		}
	}
}
