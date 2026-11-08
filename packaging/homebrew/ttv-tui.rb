class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.5.0"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.5.0/ttv-tui_0.5.0_darwin_arm64.tar.gz"
    sha256 "67e52d4ed4bba367618b49165a79041aabbc0e95067f92f8c25bd3c18c2f1e68"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.5.0/ttv-tui_0.5.0_darwin_amd64.tar.gz"
    sha256 "5ae459ee07db87e2d5ebf366552180abd6fc5949d7f38a1dea0aca4f73e7357b"
  end
  license "MIT"

  depends_on "mpv"
  depends_on "streamlink"

  def install
    bin.install "ttv-tui"
  end

  test do
    assert_match "ttv-tui", shell_output("#{bin}/ttv-tui --help")
  end
end
