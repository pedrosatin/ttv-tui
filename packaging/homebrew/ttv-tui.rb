class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.2.0"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.2.0/ttv-tui_0.2.0_darwin_arm64.tar.gz"
    sha256 "0f564b518b2f02f207dba575b5fd0d4e4e6dc5b52b013b33bf4ceb6f36843aca"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.2.0/ttv-tui_0.2.0_darwin_amd64.tar.gz"
    sha256 "7b8d7c7e5a2e0d26678bc382fc5ea45de178608b6641a7754d26f2e84b2f37a2"
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
