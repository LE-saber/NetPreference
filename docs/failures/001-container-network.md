# Failure 001: development-container GitHub DNS resolution

Stage: repository acquisition. Date: 2026-10-01.

Command: `git clone https://github.com/LE-saber/NetPreference.git /mnt/data/NetPreference`

Actual result: exit 128, `Could not resolve host: github.com`.

Impact: direct clone/push and downloading uncached Go modules through the container are unavailable. This is an execution-environment limitation, not a router or repository failure. The GitHub connector successfully initialized main at `4f26051595894a862182b28c2d89fa29de487619` and created `feat/mvp`.

Recovery: continue in a local isolated working copy, favor a dependency-free implementation, use the authorized GitHub connector for source commits and GitHub Actions for network-capable/kernel integration checks. Do not claim router validation from container-only tests. No user network configuration was changed.
