class AgentHtop < Formula
  desc "htop for AI agent fleets - live terminal dashboard for monitoring agent costs"
  homepage "https://github.com/acunningham-ship-it/agent-htop"
  version "0.1.0"

  on_macos do
    on_arm do
      url "https://github.com/acunningham-ship-it/agent-htop/releases/download/v0.1.0/agent-htop_0.1.0_darwin_arm64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_ARM64"
    end
    on_intel do
      url "https://github.com/acunningham-ship-it/agent-htop/releases/download/v0.1.0/agent-htop_0.1.0_darwin_amd64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_INTEL"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/acunningham-ship-it/agent-htop/releases/download/v0.1.0/agent-htop_0.1.0_linux_arm64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/acunningham-ship-it/agent-htop/releases/download/v0.1.0/agent-htop_0.1.0_linux_amd64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_LINUX_INTEL"
    end
  end

  def install
    bin.install "agent-htop"
  end

  def post_install
    puts "agent-htop installed!"
    puts "Usage: agent-htop --company <company-id>"
    puts "Help:  agent-htop --help"
  end

  test do
    system "#{bin}/agent-htop", "--version"
  end
end
