class Herald < Formula
  desc "Local desktop notifications from the CLI"
  homepage "https://github.com/lemomar/herald"
  url "https://github.com/lemomar/herald/archive/refs/tags/v0.1.0+2.tar.gz"
  sha256 "0d1245e42fc5a947223a8daaa8e82d62941a966fa2ffd32219a67bcf6cee4715"
  license "GPL-3.0-only"
  version "0.1.0+2"

  depends_on "go" => :build

  def install
    system "go", "build", "-ldflags=-s -w", "-o", "herald", "./cmd/herald"
    bin.install "herald"
  end

  def caveats
    <<~EOS
      Enable the shell hook to capture the previous command's exit code:

        # zsh
        eval "$(herald hook --shell zsh)"

        # bash
        eval "$(herald hook --shell bash)"

        # fish
        herald hook --shell fish | source
    EOS
  end

  test do
    system bin/"herald", "--help"
  end
end
