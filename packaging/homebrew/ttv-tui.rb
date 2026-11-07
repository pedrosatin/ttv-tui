class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.4.1"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.4.1/ttv-tui_0.4.1_darwin_arm64.tar.gz"
    sha256 "6c409f8ddd85b574e91b9a7bd801a301eccddf198a9b429149ba3c9cbd57b5ce"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.4.1/ttv-tui_0.4.1_darwin_amd64.tar.gz"
    sha256 "8ea823dc7e5cbecda4a5f31fcc2a8ea3086ca801c373e9521183f7762755f277"
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
