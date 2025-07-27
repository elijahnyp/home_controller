# Golang Projects
* use golangci-lint for security and style tests
* use latest stable version of go
* run go fmt before pushing

# Python Projects

# ESP32 Projects

# Global Preferences
* create tests
* use winget as first priority, then choco for softare management.  As a last resort download installer directly
* ask for confirmation before doing anything outside the project directory tree
* never change project files unless we're on a git branch.  Master and main should never be directly updated

# Global CI
* use github actions for CI/CD
  * if the project has a jenkins file, firmware images are build with jenkins but everything else is build with github actions
* container images are uploaded to ghcr
* prefer reusable github actions over raw commands
* configure dependabot to keep dependencies up to date
* use github cli
* never commit to main or master.  Do everything in a branch and open a PR
* helm charts are managed by argocd.  Don't use features of helm that argocd doesn't support
* securecodewarrior/gosec does not exist