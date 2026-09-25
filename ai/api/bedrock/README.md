# AWS Bedrock

The built-in provider ID is `amazon-bedrock`; its API is
`bedrock-converse-stream`. It supports streaming text, tool calls/results,
images, signed reasoning history, cancellation, retries, and token/cache usage.

## Authentication

Use either a Bedrock API key (also accepted by the app's API key settings):

```sh
export AWS_REGION=us-east-1
export AWS_BEARER_TOKEN_BEDROCK='your-bedrock-api-key'
```

Or configure the standard AWS SDK credential chain:

```sh
export AWS_PROFILE=my-profile
export AWS_REGION=eu-west-1
```

Environment access keys (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and
optional `AWS_SESSION_TOKEN`), shared profiles, SSO, web identity, container
credentials, and EC2 instance roles are resolved by the AWS SDK. For IAM roles,
set `AWS_REGION` so the app recognizes Bedrock as configured. A supplied Bedrock
API key takes precedence over AWS credentials. Region selection follows AWS
configuration, defaulting to `us-east-1` if none is configured.

Select **AWS Bedrock** in the provider/model picker and refresh the model list.
The catalog includes streaming text foundation models with on-demand inference
and active inference profiles. Select an inference profile when AWS requires
cross-region inference. Discovery requires `bedrock:ListFoundationModels` and
`bedrock:ListInferenceProfiles`; streaming requires
`bedrock:InvokeModelWithResponseStream`, plus access to the selected model.

The catalog does not expose pricing or token limits. Discovered models use a
conservative 32,000-token context estimate and 4,096 output tokens, and report
zero estimated cost. Output limits can be overridden with stream options.
Reasoning uses adaptive thinking on newer Claude models and explicit budgets on
older supported models. Other model-specific inference parameters can be supplied through
`SamplingParams` and are sent as `additionalModelRequestFields`.

For SDK callers, `ai.Model.BaseURL` overrides the runtime endpoint.
`AWS_ENDPOINT_URL_BEDROCK_RUNTIME` and `AWS_ENDPOINT_URL_BEDROCK` override the
runtime and discovery endpoints respectively; `AWS_ENDPOINT_URL` overrides both.

Protocol references:
[ConverseStream](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_ConverseStream.html),
[Bedrock API keys](https://docs.aws.amazon.com/bedrock/latest/userguide/api-keys-use.html).
