# openclaw-assistant

Private API server for the OpenClaw Assistant console.

`openclaw-assistant` is a small Go API service designed to run beside OpenClaw on a Mac mini or home server. The React frontend now lives in `../openclaw-assistant-ui`; this service protects API calls with Naver Login and forwards commands to the local OpenClaw Gateway without exposing the gateway itself to the internet.

## Features

- Go standard-library API server
- Naver OAuth login
- Signed HTTP-only session cookies
- Optional allowlist for specific Naver profile IDs
- OpenClaw Gateway command forwarding
- AdSense, Search Console, and Google Analytics API wrappers for AI-safe operations
- Trader, Website Builder, and Asset Manager workspaces
- React frontend in `openclaw-assistant-ui`
- Health check endpoint
- Makefile for local development, tests, and builds

## Architecture

```txt
Browser
  -> openclaw-assistant-ui :3000 or :5173
  -> openclaw-assistant :8080
  -> Tunnel / reverse proxy
  -> OpenClaw Gateway :18789
```

Keep OpenClaw Gateway private. Only expose this web application through HTTPS after login protection is configured.

## Requirements

- Go 1.24+
- OpenClaw Gateway running locally
- Naver Developers application for login
- HTTPS domain for production OAuth callbacks

## Quick Start

```sh
cp .env.example .env
make dev
```

Open the API:

```txt
http://localhost:8080
```

Run the React UI from `../openclaw-assistant-ui`:

```sh
npm install
npm run dev
```

Open:

```txt
http://localhost:5173
```

Run both with Docker Compose from the parent `openclaw` directory:

```sh
docker compose up --build
```

Run tests:

```sh
make test
```

Build:

```sh
make build
```

## Configuration

Configure the app with a local `.env` file:

```sh
PORT=8080
DEV=false
OPENCLAW_BASE_URL=http://localhost:18789
OPENCLAW_TOKEN=

FRONTEND_URL=https://agent.choigonyok.com
CORS_ALLOWED_ORIGINS=https://agent.choigonyok.com,http://localhost:5173,http://localhost:3000

NAVER_CLIENT_ID=
NAVER_CLIENT_SECRET=
NAVER_REDIRECT_URL=https://agent.choigonyok.com/auth/naver/callback
NAVER_ALLOWED_IDS=

SESSION_SECRET=change-this-long-random-string

GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
GOOGLE_REFRESH_TOKEN=
GOOGLE_ANALYTICS_PROPERTY_ID=535464309

KIS_APP_KEY=
KIS_APP_SECRET=
KIS_ACCOUNT_NO=
KIS_ACCOUNT_PRODUCT=01
KIS_MOCK=false

UPBIT_ACCESS_KEY=
UPBIT_SECRET_KEY=
```

Start from the example file:

```sh
cp .env.example .env
```

Values already exported in your shell take precedence over `.env` values.

| Variable | Required | Description |
| --- | --- | --- |
| `PORT` | No | HTTP port for this web app. Defaults to `8080`. |
| `DEV` | No | When `true`, bypasses Naver Login and uses a local development user. |
| `OPENCLAW_BASE_URL` | No | OpenClaw Gateway URL. Defaults to `http://localhost:18789`. |
| `OPENCLAW_TOKEN` | No | Bearer token sent to OpenClaw when set. |
| `FRONTEND_URL` | No | URL to redirect to after Naver login/logout. When empty, redirects to `/` on the current frontend origin. |
| `CORS_ALLOWED_ORIGINS` | No | Comma-separated React UI origins allowed to call the API with cookies. |
| `NAVER_CLIENT_ID` | Yes | Client ID from Naver Developers. |
| `NAVER_CLIENT_SECRET` | Yes | Client secret from Naver Developers. |
| `NAVER_REDIRECT_URL` | Yes | OAuth callback URL registered with Naver. |
| `NAVER_ALLOWED_IDS` | No | Comma-separated Naver profile IDs allowed to use the app. |
| `SESSION_SECRET` | Yes | Secret used to sign session cookies. Use a long random value. |
| `GOOGLE_CLIENT_ID` | No | OAuth client ID used by Google API wrappers. |
| `GOOGLE_CLIENT_SECRET` | No | OAuth client secret used by Google API wrappers. |
| `GOOGLE_REFRESH_TOKEN` | No | OAuth refresh token with the required Google API scopes. |
| `GOOGLE_ANALYTICS_PROPERTY_ID` | No | Default GA4 property ID used by the assistant UI and GA wrapper. `GA_PROPERTY_ID` is also accepted as a legacy alias. |
| `KIS_APP_KEY` | No | 한국투자증권 KIS Developers app key for domestic stock balance lookup. |
| `KIS_APP_SECRET` | No | 한국투자증권 KIS Developers app secret. |
| `KIS_ACCOUNT_NO` | No | KIS account number prefix used for balance lookup. |
| `KIS_ACCOUNT_PRODUCT` | No | KIS account product code. Defaults to `01`. |
| `KIS_MOCK` | No | When `true`, uses KIS mock trading transaction IDs. |
| `UPBIT_ACCESS_KEY` | No | Upbit Open API access key for crypto balance lookup. |
| `UPBIT_SECRET_KEY` | No | Upbit Open API secret key. |

If `NAVER_ALLOWED_IDS` is empty, any Naver account that completes login can access the app. For a private OpenClaw console, set it after confirming your Naver profile ID in the app header.

## Naver Login

Register this callback URL in Naver Developers for production:

```txt
https://agent.choigonyok.com/auth/naver/callback
```

The public deployment uses this domain even though the service is forwarded to localhost internally.

For local development, also register:

```txt
http://localhost:8080/auth/naver/callback
```

The app uses these routes:

| Route | Description |
| --- | --- |
| `/login/naver` | Starts Naver OAuth login. |
| `/auth/naver/callback` | Handles the OAuth callback. |
| `/logout` | Clears the local session. |

## Workspaces

The React frontend includes these workspaces:

- `Trader`
- `Website Builder`
- `Asset Manager`

The selected workspace is sent to OpenClaw through `POST /api/command` as command context:

```txt
[Trader]
...
```

```txt
[Website Builder]
...
```

```txt
[Asset Manager]
...
```

Asset Manager also shows connected portfolio balances:

| Route | Method | Purpose |
| --- | --- | --- |
| `/api/session` | `GET` | Current session and login/logout URLs. |
| `/api/command` | `POST` | Send a tab-scoped command to OpenClaw Gateway. |
| `/api/assets` | `GET` | KIS domestic stock account balance and holdings. |
| `/api/crypto` | `GET` | Upbit KRW balance and crypto holdings. |

## Google API Wrappers

Authenticated users and `DEV=true` sessions can call a small set of API routes that wrap Google APIs for AI usage.

Required OAuth scopes depend on the operation:

```txt
https://www.googleapis.com/auth/webmasters
https://www.googleapis.com/auth/webmasters.readonly
https://www.googleapis.com/auth/adsense.readonly
https://www.googleapis.com/auth/analytics.readonly
```

Available routes:

| Route | Method | Purpose |
| --- | --- | --- |
| `/api/google/status` | `GET` | Check Google wrapper configuration. |
| `/api/google/search-console/sites` | `GET` | List Search Console properties. |
| `/api/google/search-console/site` | `PUT` | Add a Search Console property. Ownership verification still happens through Google. |
| `/api/google/search-console/sitemap` | `POST` | Submit a sitemap to Search Console. |
| `/api/google/search-console/url-inspection` | `POST` | Inspect a URL in Search Console. |
| `/api/google/search-console/search-analytics` | `POST` | Query Search Console performance metrics. |
| `/api/google/adsense/accounts` | `GET` | List AdSense accounts. |
| `/api/google/adsense/sites` | `GET` | List AdSense sites for an account. |
| `/api/google/adsense/report` | `POST` | Generate an AdSense report. |
| `/api/google/analytics/run-report` | `POST` | Run a GA4 Data API report. |

Examples:

```sh
curl http://localhost:8080/api/google/status
```

```sh
curl -X POST http://localhost:8080/api/google/search-console/sitemap \
  -H 'Content-Type: application/json' \
  -d '{"site_url":"https://example.com/","sitemap_url":"https://example.com/sitemap.xml"}'
```

```sh
curl -X PUT http://localhost:8080/api/google/search-console/site \
  -H 'Content-Type: application/json' \
  -d '{"site_url":"https://example.com/"}'
```

```sh
curl -X POST http://localhost:8080/api/google/adsense/report \
  -H 'Content-Type: application/json' \
  -d '{"account":"accounts/pub-1234567890","start_date":"2026-04-01","end_date":"2026-04-29","dimensions":["DATE"],"metrics":["ESTIMATED_EARNINGS","PAGE_VIEWS","CLICKS"]}'
```

```sh
curl -X POST http://localhost:8080/api/google/analytics/run-report \
  -H 'Content-Type: application/json' \
  -d '{"property_id":"123456789","query":{"dateRanges":[{"startDate":"7daysAgo","endDate":"today"}],"dimensions":[{"name":"pagePath"}],"metrics":[{"name":"screenPageViews"},{"name":"activeUsers"}]}}'
```

AdSense site creation and general page indexing requests are not exposed as direct APIs by Google in the same way. Use AdSense for site approval, and use Search Console sitemap submission plus URL inspection for discovery and diagnostics.

## Deployment

For a Mac mini without a fixed public IP, use a tunnel or reverse proxy pattern.

Recommended with Cloudflare Tunnel:

```txt
assistant.choigonyok.com
  -> Cloudflare Tunnel
  -> Mac mini localhost:8080
  -> OpenClaw localhost:18789
```

Run the app on the Mac mini:

```sh
./bin/openclaw-assistant
```

Put the production values in `.env` on the Mac mini before starting the app.

Never expose `:18789` directly to the internet.

## Development

```sh
make run
make dev
make test
make build
make clean
```

Project layout:

```txt
cmd/openclaw-assistant/  Application entrypoint
internal/app/           Web server, auth, UI, OpenClaw client
```

## Security Notes

- Set a strong `SESSION_SECRET` in production.
- Set `NAVER_ALLOWED_IDS` for private use.
- Serve the app over HTTPS.
- Keep OpenClaw Gateway bound to localhost.
- Do not commit real OAuth secrets or tokens.
