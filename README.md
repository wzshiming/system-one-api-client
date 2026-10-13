# system-one-api-client
Go client for the System One API (TypeSafe Jev on DigitalOcean Serverless Inference) that sends a `state` with typed `questions` and returns choice, score, and noul answers with calibrated probabilities; standard library only.

## Install
```sh
go get github.com/wzshiming/system-one-api-client
```

## Usage
```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	systemone "github.com/wzshiming/system-one-api-client"
)

func main() {
	client := systemone.New(systemone.WithToken(os.Getenv("MODEL_ACCESS_KEY")))
	resp, err := client.Evaluate(context.Background(), &systemone.Request{
		Model: "typesafe-jev-1.13.0",
		State: "The customer said the delivery was late but the product quality was excellent.",
		Questions: map[string]systemone.Question{
			"sentiment": systemone.Choice("What is the overall sentiment of this feedback?", map[string]any{
				"positive": "Customer is happy overall",
				"negative": "Customer is unhappy overall",
				"mixed":    "Customer has both good and bad things to say",
			}),
			"needs_followup": systemone.Noul("Does this feedback require a follow-up from the support team?"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Answers["sentiment"].Choice, resp.Answers["sentiment"].Confidence, resp.Answers["needs_followup"].Noul)
}
```

- `DefaultBaseURL` is `https://inference.do-ai.run`; `WithBaseURL` targets another server (TypeSafe's `https://api.typesafe.ai` or a self-hosted endpoint) and `WithHTTPClient` sets the `*http.Client`.
- `Score(instructions, levels...)` builds a score question; `Request.State`, `Question.Instructions`, and criteria entries accept a string, object, or array.
- A non-2xx response returns an `*APIError` with `StatusCode`, `Type`, `Message`, and `Body`.
- 429, 5xx, and transport errors are retried twice with exponential backoff that honors `Retry-After` and `retry-after-ms`; `WithMaxRetries(0)` disables retries.

## Testing
```sh
go test ./...
# e2e against a live endpoint; skipped unless both variables are set, SYSTEMONE_TOKEN is optional
SYSTEMONE_BASE_URL=http://host:8033 SYSTEMONE_MODEL=julia-1 go test -run E2E -v ./...
```

## References
- [DigitalOcean: Send Requests Using System One API](https://docs.digitalocean.com/products/inference/how-to/use-system-one-api/)
- [TypeSafe: API reference](https://docs.typesafe.ai/api)

## License
MIT
