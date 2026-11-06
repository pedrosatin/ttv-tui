class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.3.0"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.3.0/ttv-tui_0.3.0_darwin_arm64.tar.gz"
    sha256 "3f96254abb8265a72bd2a77ca8493d40e3d39850c8311af68ca34f8ca2a9a5a0"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.3.0/ttv-tui_0.3.0_darwin_amd64.tar.gz"
    sha256 "ae07c6f5edd43d6559108cce15a4f0668c19160dcfdb86cdda6ef9a40f2fe547"
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
