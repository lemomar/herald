class Herald < Formula
  desc "Local desktop notifications from the CLI"
  homepage "https://github.com/lemomar/herald"
  url "https://github.com/lemomar/herald/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_SHA256"
  license "GPL-3.0-only"
  version "0.1.0"

  depends_on "go" => :build

  def install
    system "go", "build", "-ldflags=-s -w", "-o", "herald", "./cmd/herald"
    bin.install "herald"
  end

  test do
    system bin/"herald", "--help"
  end
end
