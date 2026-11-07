class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.4.0"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.4.0/ttv-tui_0.4.0_darwin_arm64.tar.gz"
    sha256 "611b01da39dc67d7b17c8a6aec2389375eac228f848d1fa50ea3365af047e442"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.4.0/ttv-tui_0.4.0_darwin_amd64.tar.gz"
    sha256 "527bb0a74b0a2aedf29297d521ce1bfdf24598a27e88e270f6d82667cd506375"
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
