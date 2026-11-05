class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.2.1"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.2.1/ttv-tui_0.2.1_darwin_arm64.tar.gz"
    sha256 "1702bd0f3734bdefdd7c4757e647615b1361365b37377fe2dea60ebc606efbad"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.2.1/ttv-tui_0.2.1_darwin_amd64.tar.gz"
    sha256 "b2e0295a99ce1ea5a51cf66cf8627ae370302163f46b84764d8a89c5ac999886"
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
