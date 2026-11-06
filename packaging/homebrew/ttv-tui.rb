class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.3.1"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.3.1/ttv-tui_0.3.1_darwin_arm64.tar.gz"
    sha256 "b522ca3b23966196b52ce1bb535d47861ed59c2bb12f1f8c56233b1978cdc744"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.3.1/ttv-tui_0.3.1_darwin_amd64.tar.gz"
    sha256 "762752fb6ec569939f2ea8b6238db1f7248f7f2132e966f0bbd83a12a99aa7ab"
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
