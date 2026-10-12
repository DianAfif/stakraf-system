# Design system: Stakraf System

The goal is an original operational command console, inspired by data-rich enterprise operations UIs. White surfaces, graphite controls, and neutral gray create a clear hierarchy for infrastructure decisions.

Principles: truthful live data; no fictional infrastructure counts; status before aesthetics; decision-aware workflows; no destructive change without explicit approval; compact UI with clear information hierarchy; keyboard quick switch; meaningful responsive behavior.

Tokens: background `#f7f7f8`, surface `#ffffff`, border `#dedee3`, text `#202124`, muted `#62636c`, accent `#292a30`, danger `#b42332`, warning `#8a5100`, information `#365d9c`. No green brand accent. Healthy states use neutral badges with explicit labels; incidents and pending decisions use restrained red and amber. Typeface: Plus Jakarta Sans, technical numbers IBM Plex Mono. Icons: Lucide. Keyboard focus must remain visible; reduced-motion preferences are respected.

Information architecture: Overview, Projects, Infrastructure, Network, Applications, Deployments, Telemetry, Incidents, Change Control, Audit, API & MCP, Settings. All read views display real API state, and all action affordances link to implemented API paths.
