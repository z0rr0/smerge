# SMerge

![Go](https://github.com/z0rr0/smerge/workflows/Go/badge.svg)
![Version](https://img.shields.io/github/tag/z0rr0/smerge.svg)
![License](https://img.shields.io/github/license/z0rr0/smerge.svg)

Subscriptions merge tool.

It's a web service that joins data from multiple stealth proxy subscriptions and provides it in a single endpoint.
It can decode and encode data in base64 format and supports groups update periods.

## Configuration

Simple JSON configuration file with groups and subscriptions.
Example in [config.json](https://github.com/z0rr0/smerge/blob/main/config.json):

```json
{
  "host": "localhost",
  "port": 43210,
  "user_agent": "SMerge/1.0",
  "timeout": "10s",
  "debug": true,
  "groups": [
    {
      "name": "group1",
      "endpoint": "/group1",
      "encoded": true,
      "period": "90m",
      "subscriptions": [
        {
          "name": "subscription1",
          "url": "http://localhost:43211/subscription1",
          "encoded": false,
          "timeout": "10s"
        },
        {
          "name": "subscription2",
          "url": "http://localhost:43212/subscription2",
          "encoded": true,
          "timeout": "10s"
        }
      ]
    }
  ]
}
```

`Encoded` is a flag what means that subscription data is encoded in base64.

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
ok      github.com/z0rr0/smerge         (cached)        coverage: 65.4% of statements
ok      github.com/z0rr0/smerge/cfg     (cached)        coverage: 95.4% of statements
ok      github.com/z0rr0/smerge/crawler (cached)        coverage: 92.5% of statements
ok      github.com/z0rr0/smerge/server  (cached)        coverage: 90.8% of statements
```

## Run

```bash
./smerge -help
Usage of ./smerge:
  -config string
        configuration file (default "config.json")
  -debug
        debug mode
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

Or using docker-compose:

```bash
docker compose up -d
```

## License

This source code is governed by a [MIT](https://opensource.org/license/MIT)
license that can be found in the [LICENSE](https://github.com/z0rr0/smerge/blob/main/LICENSE) file.
