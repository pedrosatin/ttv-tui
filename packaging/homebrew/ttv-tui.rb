class TtvTui < Formula
  desc "Browse and watch Twitch in the terminal"
  homepage "https://github.com/pedrosatin/ttv-tui"
  url "https://github.com/pedrosatin/ttv-tui/archive/37b50d0c4d9d2f8b17be35f4dc5ac241cbb9a4d3.tar.gz"
  version "0.0.0"
  sha256 "03034c030f7313bab69535279d30744aefca05fe3357533ef759831b46579bfc"
  license "MIT"

  depends_on "go" => :build
  depends_on "mpv"
  depends_on "streamlink"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}"), "./cmd/ttv-tui"
  end

  test do
    assert_match "ttv-tui", shell_output("#{bin}/ttv-tui --help")
  end
end
