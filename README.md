# SMerge

![Go](https://github.com/z0rr0/smerge/workflows/Go/badge.svg)
![Version](https://img.shields.io/github/tag/z0rr0/smerge.svg)
![License](https://img.shields.io/github/license/z0rr0/smerge.svg)

Subscriptions merge tool.

It's a web service that joins data from multiple stealth proxy subscriptions and provides it in a single endpoint.
It can decode and encode data in base64 format and supports groups update periods.

## Build

```bash
make build
```

Or using docker:

```bash
make docker
```

Test coverage:

```bash
make test
...
ok      github.com/z0rr0/smerge         (cached)        coverage: 56.7% of statements
ok      github.com/z0rr0/smerge/cfg     (cached)        coverage: 92.9% of statements
ok      github.com/z0rr0/smerge/crawler (cached)        coverage: 91.4% of statements
ok      github.com/z0rr0/smerge/server  (cached)        coverage: 96.6% of statements
```

## Run

```bash
./smerge -help
Usage of ./smerge:
  -config string
        configuration file (default "config.json")
  -dev
        development mode
  -version
        show version
```

Or using docker

- directory `data` on host should contain `config.json`
- port `43210` on host should be free
- `user` ID can be changed to the current user ID on the host

```bash
docker run -d \
  --name smerge \
  --user 1000:1000 \
  -p 43210:43210 \
  -v $(pwd)/data:/data:ro \
  --restart unless-stopped \
  z0rr0/smerge:latest
```

Or using [docker-compose](https://github.com/z0rr0/smerge/blob/main/docker-compose.yml):

```bash
docker compose up -d
```

## Configuration

An example of JSON configuration file can be found in
[config.json](https://github.com/z0rr0/smerge/blob/main/config.json).

### Command Line Flags

- `-version`: Show version information
- `-dev`: Enable development mode
- `-config`: Path to configuration file (default: "config.json")

### Main Configuration

- `host` (string): Hostname for the server
- `port` (uint16): Port for the server
- `user_agent` (string): User agent string for HTTP requests
- `timeout` (Duration): Global timeout for requests
- `docker_volume` (string, optional): Docker volume path for local subscriptions
- `retries` (uint8): Number of retries for failed requests
- `max_concurrent` (int, min: 1): Maximum number of concurrent subscription goroutines
- `debug` (bool): Enable debug mode
- `groups` ([]Group): Array of subscription groups

### Group Configuration

- `name` (string): Name of the group (must be unique)
- `endpoint` (string): HTTP endpoint for the group (must be unique)
- `encoded` (bool): Whether the group response should be encoded
- `period` (Duration, min: 1s): Refresh period for the group
- `subscriptions` ([]Subscription): Array of subscriptions for the group

### Subscription Configuration

- `name` (string): Name of the subscription (must be unique within a group)
- `url` (string): URL or file path of the subscription
- `encoded` (bool): Whether the subscription data is encoded
- `timeout` (Duration, min: 10ms): Timeout for subscription requests
- `has_prefixes` ([]string, optional): List of prefixes to filter subscription values
- `local` (bool): Whether the subscription is a local file

### Special Types

- `Duration`: Custom type for time durations, specified as strings like "10s", "1h", "1h30m"

### Constraints

- Minimum period for group refresh: 1 second
- Minimum timeout for subscription refresh: 10 milliseconds
- Local subscriptions require a docker_volume to be specified
- Group names and endpoints must be unique
- Subscription names must be unique within a group

## License

This source code is governed by a [MIT](https://opensource.org/license/MIT)
license that can be found in the [LICENSE](https://github.com/z0rr0/smerge/blob/main/LICENSE) file.
