module github.com/mikus/maiku

go 1.26.6

require (
	github.com/bmatcuk/doublestar/v4 v4.10.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/takara-ai/miru-code v0.0.0
	golang.org/x/text v0.39.0
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/net v0.54.0

replace github.com/takara-ai/miru-code => ./miru-code
