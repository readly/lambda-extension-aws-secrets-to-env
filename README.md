# lambda-extension-aws-secrets-to-env
AWS Lambda extension that looks for environment variables suffixed with `SECRET_ARN` and looks them up in AWS Secret manager and writes the key and value to `/tmp/.env`.
