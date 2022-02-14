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

	"github.com/readly/lambda-extension-aws-secrets-to-env/extension"
)

var (
	extensionName   = filepath.Base(os.Args[0]) // extension name has to match the filename
	extensionClient = extension.NewClient(os.Getenv("AWS_LAMBDA_RUNTIME_API"))
	printPrefix     = fmt.Sprintf("[%s]", extensionName)
)

const envFile = "/tmp/.env"

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sigs
		cancel()
		println(printPrefix, "Received", s)
		println(printPrefix, "Exiting")
	}()

	// fetch secrets from AWS Secrets Manager
	secretsToEnvFile()

	res, err := extensionClient.Register(ctx, extensionName)
	if err != nil {
		panic(err)
	}
	println(printPrefix, "Register response:", prettyPrint(res))

	// Will block until shutdown event is received or cancelled via the context.
	processEvents(ctx)
}

func secretsToEnvFile() {
	if _, err := os.Stat(envFile); err == nil {
		println(printPrefix, ".env file already exists, skipping...")
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
			println(printPrefix, "Found secret arn", envName)
			secretsMap, err := GetSecret(envValue)
			if err != nil {
				panic(err)
			}
			for k, v := range secretsMap {
				println(printPrefix, "Writing", k, "to env file")
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
		return nil, fmt.Errorf("Error: %s\n", err)
	}

	return secretsMap, nil
}

func processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			println(printPrefix, "Waiting for event...")
			res, err := extensionClient.NextEvent(ctx)
			if err != nil {
				println(printPrefix, "Error:", err)
				println(printPrefix, "Exiting")
				return
			}
			println(printPrefix, "Received event:", prettyPrint(res))
			// Exit if we receive a SHUTDOWN event
			if res.EventType == extension.Shutdown {
				println(printPrefix, "Received SHUTDOWN event")
				println(printPrefix, "Exiting")
				return
			}
		}
	}
}

func prettyPrint(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return ""
	}
	return string(data)
}
