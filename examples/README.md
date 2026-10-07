# Examples

A synthetic job posting and resume (the company and the candidate are
fictional), the real output of every command on them, and the Jev
recording that produced it. Nothing here needs an API key.

| File | What it is |
|---|---|
| [`job.md`](job.md) | Job description: Senior Backend Engineer (Go) at a freight platform |
| [`resume.md`](resume.md) | Resume: backend engineer, 6 years, 3 in Go, no Kafka or Terraform |
| [`score.json`](score.json) | `score` output: Match Score 86 |
| [`requirements.json`](requirements.json) | `extract` output: 30 Requirements with Tier, Importance, Alternative Groups |
| [`coverage.json`](coverage.json) | `check` output: Coverage per Requirement with quoted evidence, Fit Score 75 |
| [`jev-recording.json`](jev-recording.json) | Every Jev request and answer from that run (67 calls, $0.0055) |

## Run them without a key

`JEV_REPLAY` answers every Jev request from the recording instead of the
API, so these run offline and reproduce the files above exactly:

```sh
make build
export JEV_REPLAY=examples/jev-recording.json OPENAI_MODEL=

bin/score -jd examples/job.md -resume examples/resume.md
bin/extract examples/job.md
bin/check -requirements examples/requirements.json -resume examples/resume.md
```

A replay only answers requests it has seen. Change a word in either file
(or use your own) and the command stops with "request not in the
recording": that needs a real key. `OPENAI_MODEL=` keeps the generative
model off, because the recording was made with the built-in fallback Job
Summary.

## What the output shows

- **Score 86**: right kind of role (`role_match` 0.99), not short on
  experience (`experience_short` 0.15), no blocker (0.05).
- **Strong evidence** for Go, gRPC/REST, PostgreSQL schema design and
  query tuning, Kubernetes on AWS, Prometheus and Grafana, each quoting the
  resume line it came from.
- **Partial** for event streaming: the resume has RabbitMQ, the job asks
  for Kafka "or similar".
- **None** for Terraform, SLOs, and incident reviews: the resume never
  mentions them. Terraform is a preferred item, so it shows in the Fit
  Score but is not a Gap.
- **Alternatives count once.** GCP and OpenTelemetry are none, but each
  sits in an Alternative Group with something the resume has (AWS;
  Prometheus and Grafana), so the group is covered.
- **One miss**: "logistics" is none, although the candidate works at a
  shipment-tracking company. The checker links requirements to resume
  lines; it does not infer an industry from a company name. The group it
  belongs to (logistics, marketplaces, or pricing systems) is still covered
  through the pricing export.

## Record your own

With `TYPESAFE_API_KEY` set, `JEV_RECORD=<file>` saves every exchange of a
real run, and `JEV_REPLAY=<file>` plays it back later. `make examples`
re-records this directory; run it after changing prompts, `tuning.yaml`, or
the pipeline. `go test ./cmd/...` replays the recording and fails when it no
longer matches.
