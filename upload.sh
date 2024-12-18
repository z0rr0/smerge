#!/usr/bin/env bash

# extract the latest git tag
TAG=$(git tag | sort -V | tail -1)
if [ -z "$TAG" ]; then
  TAG="v0.0.0"
fi

VERSION="${TAG:1}"
if [ -z "$VERSION" ]; then
  echo "error: not valid version"
  exit 1
fi

echo "version: ${VERSION}"

# add tags
if ! docker tag z0rr0/smerge:latest z0rr0/smerge:"${VERSION}"
then
  echo "error: failed to tag Docker image"
  exit 1
fi

# push the versioned image to Docker Hub
if ! docker push z0rr0/smerge:"${VERSION}"
then
  echo "error: failed to push versioned Docker image"
  exit 1
fi

# push the latest image to Docker Hub
if ! docker push z0rr0/smerge:latest
then
  echo "error: failed to push latest Docker image"
  exit 1
fi

echo "Docker images pushed successfully"