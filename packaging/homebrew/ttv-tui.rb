class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  version "0.9.0"

  on_arm do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.9.0/ttv-tui_0.9.0_darwin_arm64.tar.gz"
    sha256 "c2178fbd0933c104ee87de931da637760040f4bf9fbcf61071be9e85955d862b"
  end

  on_intel do
    url "https://github.com/pedrosatin/ttv-tui/releases/download/v0.9.0/ttv-tui_0.9.0_darwin_amd64.tar.gz"
    sha256 "df2772424650947b856da0c50403eb47ce1cc4955b721adaa25bf81da31f50b8"
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
