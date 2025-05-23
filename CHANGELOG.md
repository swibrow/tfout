# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial implementation of Terraform Outputs Operator
- Support for multiple S3 backends 
- Automatic sync of Terraform outputs to ConfigMaps and Secrets
- ETag-based change detection for efficient syncing
- Sensitive/non-sensitive output separation
- Conflict resolution for overlapping output keys
- Comprehensive GitHub Actions CI/CD workflows
- Security scanning with govulncheck, gosec, and CodeQL
- Multi-architecture container images (amd64, arm64)
- E2E tests with Kind cluster
- Automated release creation with artifacts

### Changed
- N/A

### Deprecated
- N/A

### Removed
- N/A

### Fixed
- N/A

### Security
- N/A

## Template for future releases

## [X.Y.Z] - YYYY-MM-DD

### Added
- New features

### Changed
- Changes in existing functionality

### Deprecated
- Soon-to-be removed features

### Removed
- Removed features

### Fixed
- Bug fixes

### Security
- Security improvements