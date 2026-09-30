# depllo-go

Typed Go client for [Depllo](https://depllo.forjio.com) — GitLab-CI-style CI/CD for
your GitHub repos.

```bash
go get github.com/hachimi-cat/depllo-go
```

```go
import (
	"context"
	"os"

	depllo "github.com/hachimi-cat/depllo-go"
)

// Bearer token from Config.Token or the DEPLLO_TOKEN env var — a workspace API key
// (sk_live_…, Dashboard → API Keys). Base URL defaults to https://depllo.forjio.com/api/v1.
c := depllo.New(depllo.Config{Token: os.Getenv("DEPLLO_TOKEN")})
ctx := context.Background()

raw, err := c.API.ProjectsList(ctx)
var projects []struct{ ID, Name string }
err = depllo.Decode(raw, &projects)

// Run a pipeline
_, err = c.API.ProjectsCreatePipelines(ctx, projects[0].ID, &depllo.ProjectsCreatePipelinesArgs{
	Ref:       "main",
	Variables: map[string]any{"DEPLOY_ENV": "staging"},
})

// A job's log, from chunk 5
raw, err = c.API.JobsLog(ctx, "job_01hx…", &depllo.JobsLogArgs{From: 5})

// Download an artifact — bytes come back as a File
raw, err = c.API.JobsArtifactsDownload(ctx, "job_01hx…", "art_01hx…")
var zip depllo.File
err = depllo.Decode(raw, &zip)
```

`Client.API` has one method per Depllo feature route, generated from the API spec
(`scripts/apigen.sh`) — the same routes as the JS and Python SDKs and the CLI's
`depllo api` commands; the [API reference](https://depllo.forjio.com/docs/api/reference)
lists them. Each takes the path parameters, then an `*<Method>Args` (required fields
plain values, optional ones pointers — `depllo.Ptr(v)`).

Like the JS and Python SDKs, every call returns the whole Forjio envelope
`{data, error, meta}` as `json.RawMessage`, so a list's `meta.cursor` / `meta.hasMore`
is kept: `depllo.Decode(raw, &out)` reads the data, `depllo.DecodeEnvelope(raw)` the
rest. Mutating calls carry an `Idempotency-Key`. Every failure is a `*depllo.Error`
(`Status`, `Code`, `Message`, `RequestID`).
