# Growing Pains

Use this document to record real incidents, broken assumptions, adapter failures,
prediction mistakes, and the fixes that made `athena` more operationally solid.

## 2026-04-01

- The first image build failed because `go.mod` required Go `>= 1.23` while the
  Dockerfile still used `golang:1.22`. The fix was to align the builder image
  with the actual module toolchain requirement before retrying the build.

- The first safe GitOps activation started at `replicas: 0` until a real image,
  smoke test, and digest pin existed. This prevented turning an unverified
  placeholder into a broken live deployment.

- The first GHCR-published image was single-platform only. It deployed cleanly
  for the cluster path, but local smoke on an arm64 Mac failed with `no matching
  manifest for linux/arm64/v8`. The fix is to either publish a multi-arch image
  or explicitly test the amd64 image through emulation when local validation is
  required.
