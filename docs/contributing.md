# Contributing to Hardn

Thank you for your interest in contributing to Hardn! This document outlines the process for contributing to the project and provides guidance on development workflow, code standards, and the release process.

## Table of Contents

- [Development Setup](#development-setup)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)
- [Issue Reporting](#issue-reporting)

## Development Setup

1. **Fork and clone the repository**

   ```bash
   git clone https://github.com/abbott/hardn.git
   cd hardn
   ```

2. **Install dependencies**

   ```bash
   go mod download
   ```

3. **Verify the setup**

   ```bash
   make test
   make build
   ```

4. **Ensure you're in development mode**

   ```bash
   make restore-dev
   ```
   
   This adds a `replace` directive to your go.mod file that points to your local version of the code for development.

## Development Workflow

1. **Create a new branch for your feature or bugfix**

   ```bash
   git checkout -b feature/your-feature-name
   # or
   git checkout -b fix/your-bugfix-name
   ```

2. **Make your changes and write tests**

3. **Run tests to ensure everything works**

   ```bash
   make test
   ```

4. **Build the project**

   ```bash
   make build
   ```

5. **Commit your changes with clear commit messages**

   ```bash
   git add .
   git commit -m "type: description of your change"
   ```

   Commit message types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `build`

## Code Style

- Follow standard Go code style guidelines and idiomatic Go
- Use `gofmt` or `goimports` to format your code
- Write comprehensive comments and documentation
- Aim for clear, readable, and maintainable code
- Include tests for new functionality

## Pull Request Process

1. **Update your fork with the latest changes from the main repository**

   ```bash
   git remote add upstream https://github.com/abbott/hardn.git
   git fetch upstream
   git merge upstream/main
   ```

2. **Push your branch to your fork**

   ```bash
   git push origin your-branch-name
   ```

3. **Create a pull request**
   - Go to the [Hardn repository](https://github.com/abbott/hardn)
   - Click "New Pull Request"
   - Choose your fork and the branch you created
   - Fill in the PR template with details about your changes

4. **Address review feedback**
   - Make additional commits to address feedback
   - Keep the PR focused on a single change

5. **Your PR will be merged once approved**

## Release Process

Hardn follows semantic versioning (MAJOR.MINOR.PATCH) and uses a specific workflow to manage releases.

### Development vs. Release Mode

During development, we use a `replace` directive in `go.mod` to point to the local code:

```
replace github.com/abbott/hardn => ./
```

For releases, this directive must be removed to ensure users get the correct version from the Go module proxy.

### Local Release Preparation

1. **Bump the version** (choose one based on the changes):

   ```bash
   make bump-patch  # For bug fixes and minor changes
   make bump-minor  # For new features
   make bump-major  # For breaking changes
   ```

2. **Prepare for release** (removes the replace directive):

   ```bash
   make prepare-release
   ```

3. **Create a release**:

   ```bash
   make release
   ```

   This will:
   - Run tests
   - Build for all target platforms
   - Create archives
   - Generate checksums
   - Tag the release
   - Push the tag

4. **Return to development mode**:

   ```bash
   make restore-dev
   ```

### Automated Releases via CI/CD

When a new tag is pushed, GitHub Actions will automatically:
1. Remove the replace directive
2. Build release artifacts
3. Create a GitHub release
4. Publish packages

### Creating Distribution Packages

To create distribution packages:

```bash
# Create a Debian package
make deb

# Create an RPM package
make rpm
```

Note: This requires the `fpm` tool to be installed.

## Issue Reporting

- Use the GitHub issue tracker to report bugs
- Provide detailed reproduction steps
- Include your environment details (OS, Go version, etc.)
- For security vulnerabilities, please email `641138+abbott@users.noreply.github.com` instead of creating a public issue.

---

Thank you for contributing to Hardn! Your efforts help make this tool better for everyone.