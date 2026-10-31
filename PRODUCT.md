# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

**Primary: the operator.** One administrator runs the bot for a small circle of colleagues. They open the Mini App from Telegram on a phone, usually standing or between meetings, to answer one question: *did anything relevant come in, and does the right person have access?*

**Secondary: allow-listed colleagues.** A handful of trusted people use the same surface for the same data with fewer permissions: they search notices, manage their own keywords, and review alarm records. They never see other people's rows or the settings screen.

## Product Purpose

The bot crawls public procurement notice sites on a schedule, matches them against each user's keywords, pushes hits to Telegram, and keeps the records. The Mini App exists because the chat cannot do the three things users need repeatedly: search a large history, edit keywords in bulk, and look up the detail of an alarm record without scrolling back through messages.

Success is a user closing the Mini App within seconds, having either found the notice or confirmed that nothing needs attention.

## Positioning

The data and the notifications already live in the operator's own bot and its SQLite database; the Mini App is a control surface over that store, not a new web product. It has no account system of its own: identity comes from Telegram `initData`, and the same allow-list gates the bot and the API.

## Operating Context

- Opened inside the Telegram client (mobile and desktop webviews), often on a mobile network, session measured in seconds.
- Lives behind a public HTTPS URL because Telegram requires one; the deployment target is small and self-hosted.
- Conversion (notice → PDF/PNG) runs through a Gotenberg instance and delivers the file into the chat, not into the app.
- The repository is public: source, tests and fixtures carry synthetic data only. Real notice content, user ids and hostnames stay out of committed files.
- No build step for the front end: HTML, CSS and JS are embedded in the Go binary with `go:embed`, and the Telegram WebApp SDK is vendored rather than loaded from a CDN.

## Capabilities and Constraints

- Endpoints: statistics, notices (date range + search + paging), keywords CRUD, alarm keywords CRUD, alarm records (search + paging), conversion, `/api/me`, admin people and settings.
- Access modes: `restricted` (only listed people) and `off` (anyone). The administrator is seeded from `MANAGER_ID` and cannot be removed or demoted.
- Authentication is Telegram `initData` signed with the bot token; no cookies, no sessions, no passwords.
- The chat still owns push notifications, `/settings` entry and the conversion result; the app never sends messages on its own except through an explicit "open in chat" link.

## Brand Commitments

- The product name is **magnet**; the bot presents itself as a personal instrument, not a SaaS product.
- The interface language is English, terse and literal. Controls name their action; errors name the problem and the recovery.
- The app must look like it belongs to the Telegram client it is opened from: it follows the client's light/dark theme and never fights the surrounding chrome.

## Evidence on Hand

- Real operating data (tens of thousands of notices, dozens of keywords, hundreds of alarm records) exists only in the operator's local database and must never be committed.
- No logo, illustration, marketing copy or customer evidence exists, and none may be invented.

## Product Principles

1. **One question per screen.** Each tab answers a single question; anything else belongs on another tab.
2. **Seconds, not sessions.** Optimistic UI is unnecessary; latency is. Prefer one request, paged, over a rich first paint.
3. **Trust is visible.** Access state, who is allowed and why, is always one tap away and always written in words, not only implied by a switch.
4. **Native over novel.** The client's theme, the platform's type stack and familiar control patterns win over brand expression.
5. **The chat does the talking.** Notifications, confirmations and converted files arrive as messages; the app is for browsing and editing.

## Accessibility & Inclusion

- Touch targets at least 40px, contrast at least 4.5:1 for body text (the theme's own accent is re-derived when it fails), and every state expressed in words as well as colour.
- Dark theme is a first-class state, not an afterthought.
