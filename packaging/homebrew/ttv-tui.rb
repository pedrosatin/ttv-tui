class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.5.1"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.5.1/ttv-tui_0.5.1_darwin_arm64.tar.gz"
    sha256 "bc3219a47d1aaf9e18438cb0693710eb3658251ff7b694c88b47057a1af1ee7b"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.5.1/ttv-tui_0.5.1_darwin_amd64.tar.gz"
    sha256 "ca95feb6d88058405be4d77eedf56c3fc71a75a06bed3f3d3188e3fac7543123"
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
