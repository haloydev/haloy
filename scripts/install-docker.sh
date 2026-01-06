#!/usr/bin/env bash

set -e

# Docker installation script for Haloy.
#
# This script installs Docker Engine using the official package repository
# method recommended by Docker for production environments.
#
# Supported distributions:
#   - Ubuntu (20.04+)
#   - Debian (10+)
#   - CentOS (8+)
#   - RHEL (8+)
#   - Fedora (38+)
#   - Alpine (3.14+)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/haloydev/haloy/main/scripts/install-docker.sh | sh
#
# Requirements:
#   - Root privileges (run as root or with sudo)
#   - curl or wget
#   - Internet connectivity

# --- Helper functions ---

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

error() {
    echo "ERROR: $1" >&2
    exit 1
}

info() {
    echo "INFO: $1"
}

warn() {
    echo "WARN: $1" >&2
}

# --- Check prerequisites ---

if [ "$(id -u)" -ne 0 ]; then
    error "This script must be run as root. Please run with: sudo sh install-docker.sh"
fi

# Check if Docker is already installed
if command_exists docker; then
    DOCKER_VERSION=$(docker --version 2>/dev/null || echo "unknown")
    info "Docker is already installed: $DOCKER_VERSION"
    info "Skipping installation."
    exit 0
fi

# --- Detect distribution ---

if [ -f /etc/os-release ]; then
    # shellcheck source=/dev/null
    . /etc/os-release
    DISTRO="$ID"
    VERSION_ID="${VERSION_ID:-}"
    VERSION_CODENAME="${VERSION_CODENAME:-}"
else
    error "Cannot detect distribution. /etc/os-release not found.
Please install Docker manually following the official documentation:
https://docs.docker.com/engine/install/"
fi

info "Detected distribution: $DISTRO (version: ${VERSION_ID:-unknown})"

# --- Installation functions for each distribution ---

install_docker_debian_ubuntu() {
    info "Installing Docker using official apt repository..."

    # Install prerequisites
    apt-get update
    apt-get install -y ca-certificates curl gnupg

    # Add Docker's official GPG key
    install -m 0755 -d /etc/apt/keyrings
    if [ -f /etc/apt/keyrings/docker.gpg ]; then
        rm /etc/apt/keyrings/docker.gpg
    fi
    curl -fsSL "https://download.docker.com/linux/$DISTRO/gpg" | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg

    # Determine codename - use VERSION_CODENAME if available, otherwise try to detect
    CODENAME="$VERSION_CODENAME"
    if [ -z "$CODENAME" ]; then
        # Fallback for older systems or derivatives
        if command_exists lsb_release; then
            CODENAME=$(lsb_release -cs)
        fi
    fi

    if [ -z "$CODENAME" ]; then
        error "Cannot determine distribution codename. Please install Docker manually:
https://docs.docker.com/engine/install/$DISTRO/"
    fi

    # Add the repository
    ARCH=$(dpkg --print-architecture)
    echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$DISTRO $CODENAME stable" > /etc/apt/sources.list.d/docker.list

    # Install Docker
    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin

    # Enable and start Docker
    if command_exists systemctl; then
        systemctl enable docker
        systemctl start docker
    fi
}

install_docker_centos_rhel() {
    info "Installing Docker using official yum/dnf repository..."

    # Determine package manager
    if command_exists dnf; then
        PKG_MGR="dnf"
    elif command_exists yum; then
        PKG_MGR="yum"
    else
        error "Neither dnf nor yum found. Cannot install Docker."
    fi

    # Remove old versions if present
    $PKG_MGR remove -y docker docker-client docker-client-latest docker-common \
        docker-latest docker-latest-logrotate docker-logrotate docker-engine 2>/dev/null || true

    # Install prerequisites
    $PKG_MGR install -y yum-utils

    # Add Docker repository
    # For RHEL, we use the CentOS repository as Docker recommends
    REPO_DISTRO="$DISTRO"
    if [ "$DISTRO" = "rhel" ]; then
        REPO_DISTRO="centos"
    fi

    yum-config-manager --add-repo "https://download.docker.com/linux/$REPO_DISTRO/docker-ce.repo"

    # Install Docker
    $PKG_MGR install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin

    # Enable and start Docker
    if command_exists systemctl; then
        systemctl enable docker
        systemctl start docker
    fi
}

install_docker_fedora() {
    info "Installing Docker using official dnf repository..."

    # Remove old versions if present
    dnf remove -y docker docker-client docker-client-latest docker-common \
        docker-latest docker-latest-logrotate docker-logrotate docker-selinux \
        docker-engine-selinux docker-engine 2>/dev/null || true

    # Install prerequisites
    dnf install -y dnf-plugins-core

    # Add Docker repository
    dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo

    # Install Docker
    dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin

    # Enable and start Docker
    if command_exists systemctl; then
        systemctl enable docker
        systemctl start docker
    fi
}

install_docker_alpine() {
    info "Installing Docker using Alpine packages..."

    # Update package index
    apk update

    # Install Docker
    apk add docker docker-cli

    # Enable Docker service with OpenRC
    if command_exists rc-update; then
        rc-update add docker boot
    fi

    # Start Docker
    if command_exists service; then
        service docker start
    elif [ -x /etc/init.d/docker ]; then
        /etc/init.d/docker start
    fi
}

# --- Main installation logic ---

case "$DISTRO" in
    ubuntu|debian|linuxmint|pop|elementary|zorin)
        # Ubuntu and Debian-based distributions
        if [ "$DISTRO" != "ubuntu" ] && [ "$DISTRO" != "debian" ]; then
            warn "Detected $DISTRO (Debian/Ubuntu derivative). Using Debian/Ubuntu installation method."
            # Map derivatives to their base
            case "$DISTRO" in
                linuxmint|pop|elementary|zorin)
                    # These are Ubuntu-based
                    DISTRO="ubuntu"
                    ;;
                *)
                    DISTRO="debian"
                    ;;
            esac
        fi
        install_docker_debian_ubuntu
        ;;
    centos|rocky|almalinux)
        # CentOS and RHEL-compatible distributions
        if [ "$DISTRO" != "centos" ]; then
            warn "Detected $DISTRO (CentOS-compatible). Using CentOS installation method."
            DISTRO="centos"
        fi
        install_docker_centos_rhel
        ;;
    rhel)
        install_docker_centos_rhel
        ;;
    fedora)
        install_docker_fedora
        ;;
    alpine)
        install_docker_alpine
        ;;
    *)
        error "Unsupported distribution: $DISTRO

Haloy supports the following distributions for automated Docker installation:
  - Ubuntu (20.04+)
  - Debian (10+)
  - CentOS (8+)
  - RHEL (8+)
  - Rocky Linux
  - AlmaLinux
  - Fedora (38+)
  - Alpine Linux (3.14+)

Please install Docker manually following the official documentation:
https://docs.docker.com/engine/install/

Or use the Docker convenience script (not recommended for production):
curl -fsSL https://get.docker.com | sh"
        ;;
esac

# --- Verify installation ---

if command_exists docker; then
    DOCKER_VERSION=$(docker --version)
    info "Docker installed successfully: $DOCKER_VERSION"
else
    error "Docker installation completed but 'docker' command not found. Please check the installation."
fi

# Verify Docker daemon is running
if docker info >/dev/null 2>&1; then
    info "Docker daemon is running."
else
    warn "Docker daemon may not be running. You may need to start it manually."
fi

info "Docker installation complete!"
