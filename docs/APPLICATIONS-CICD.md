# Application management and CI/CD

Open an application in **Applications** for logs, approved start/stop/restart, releases and pipeline setup. Stakraf manages single-container applications on connected Linux servers; source builds and tests run in your existing CI provider.

## Delivery workflow

Build and test source in CI, publish an image for your host's CPU architecture, then submit its immutable `repository@sha256:64-hex-digest`. In **Applications → application → Releases & CI/CD**, review the image, commit and note and approve or reject it. Submission alone never executes work.

Approval requires an online Docker-ready connector **0.5.0 or newer** and no active jobs for that application. Re-run the normal installer without `--reconnect` to upgrade while preserving enrollment.

The connector pulls before stopping the existing application, retains its container, replaces it using the existing ports/resource/security settings, and checks startup. Launch failure or early exit triggers an attempt to restore the previous container. If restoration or cleanup fails, execution output identifies the retained backup container for operator inspection. Successful completion updates the application's image. Startup checks do not validate HTTP readiness or business functions and cannot detect later crashes.

This is a single-host stop/start upgrade **with downtime**, not blue/green or zero-downtime routing. Initial deployment remains in Deployments history; digest release history starts with the first submitted release.

## Connect GitHub Actions

Create a key in the application's release section and save it once as the application's repository secret `STAKRAF_PIPELINE_TOKEN`. This credential is application-bound and submission-only: no workspace reads, approval, execution or credential administration. Rotation invalidates the old key; revocation blocks new submissions but does not cancel pending releases.

Copy `deploy/examples/application-ci.yml` to the application repository's `.github/workflows/stakraf.yml` and copy `scripts/ci-submit.mjs` into its scripts folder. Set repository variable `STAKRAF_URL` to your HTTPS console origin. Adapt the test command (`npm ci && npm test` in the Node example), Dockerfile and build platform to your project. The workflow builds/tests externally, publishes to GHCR and submits the image digest; it does not approve deployment. Publishing runs on main pushes/manual dispatch, not fork pull requests.

A real source repository, working tests/Dockerfile, registry permissions, CI secret and reachable HTTPS console are prerequisites. This template is not an already-configured connection to your repository. The publishing pattern follows [GitHub's official guidance](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images). Provider build/test logs remain in CI; Stakraf records release metadata and host execution output.

Private images require preconfigured registry credentials for the connector's dedicated service user. Stakraf does not store or distribute registry passwords. Use a read-only registry credential and standard-input password handling on the server; never put passwords in prompts, repository files or command arguments.

## API

Owner/full-account authenticated routes:

- `GET|POST /api/apps/{id}/releases`: latest 100 records or new pending release.
- `POST /api/apps/{id}/releases/{release}/decision`: `{"decision":"approve"}` or `reject`.
- `GET|POST|DELETE /api/apps/{id}/pipeline-key`: enabled state, create/rotate (secret once), revoke.

Submission-only route: `POST /api/pipeline/releases` with `Authorization: Bearer sci_...` and this body:

```json
{
  "image": "ghcr.io/team/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "commit_sha": "0123456789abcdef0123456789abcdef01234567",
  "note": "Tested release",
  "request_id": "unique-ci-run-id"
}
```

Request IDs are 8–100 characters. Repeated exact submissions for an application return the same release; conflicting reuse returns 409. Repeated approval cannot enqueue another job. AI/MCP keys cannot use these REST routes.

## Rollback and recovery

**Prepare rollback proposal** on a successful release fills its digest into a new proposal, which still needs submission and approval. Rollback changes only the container image, not database migrations, data or external side effects. Back up data and design backward-compatible migrations separately.

Release-job retry is disabled. Failed/cancelled/unknown upgrades require inspection and a fresh approved proposal. If a connector disappears, its execution lease expires without automatic replay. Inspect the host and any `stakraf-{app}-previous-{job}` backup container before proceeding.

Not included: managed source builds, GitHub App/OIDC installation, auto-approval, branch previews, secret/environment-variable distribution, volumes/databases, multi-container orchestration, autoscaling and HTTP readiness gates. The supported CI/CD path is externally built images with human-gated delivery.
