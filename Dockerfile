# Built by GoReleaser (dockers_v2): the binaries are cross-compiled first
# and copied in per platform, so this file has no build stage.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/score $TARGETPLATFORM/extract $TARGETPLATFORM/check /usr/local/bin/
# The examples and their Jev recording, for a no-key demo:
#   docker run --rm -e JEV_REPLAY=/examples/jev-recording.json IMAGE \
#     score -jd /examples/job.md -resume /examples/resume.md
COPY --chmod=0755 examples /examples

WORKDIR /work
USER nonroot:nonroot
CMD ["score", "-h"]
