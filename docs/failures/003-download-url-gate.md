# Failure 003: optional local workflow-copy download blocked

Date: 2026-10-01. The container download helper refused the public raw workflow URL because it had not first been opened through its web-view allowlist. No download occurred and no source or router state was changed.

Impact: this optional shortcut cannot be used to mirror the just-created GitHub workflow into the local source bundle as attempted. It does not invalidate the repository workflow or the passing compiled MVP tests.

Recovery: continue publishing the verified source through the authorized GitHub connector; keep CI as the network-capable kernel/build execution surface. Do not invent a successful download or rely on an absent local workflow file.
