package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/emx-mail/cli/pkgs/patchwork"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(0)
	}

	var err error
	switch args[0] {
	case "am":
		err = cmdAM(args[1:])
	case "shazam":
		err = cmdShazam(args[1:])
	case "prep":
		err = cmdPrep(args[1:])
	case "diff":
		err = cmdDiff(args[1:])
	case "mbox":
		err = cmdMbox(args[1:])
	case "-version", "--version":
		fmt.Printf("emx-b4 v%s\n", version)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知命令 %q\n\n", args[0])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`emx-b4 - 基于邮件的 Git 补丁工作流工具

用法:
  emx-b4 <command> [options]

命令:
  am       从 mbox 文件创建 git-am 可用的补丁
  shazam   直接将 mbox 中的补丁应用到当前仓库
  prep     准备补丁系列用于提交
  diff     显示两个补丁系列版本之间的差异
  mbox     解析并显示 mbox 文件信息

选项:
  -version    显示版本
  -h, --help  显示帮助`)
}

// ──────────────────────────────────────────────
// am: 创建 git-am 可用的 mbox
// ──────────────────────────────────────────────

func cmdAM(args []string) error {
	var (
		mboxFile    string
		output      string
		revision    int
		threeWay    bool
		addLink     bool
		linkPrefix  string
		addMsgID    bool
		coverTrails bool
	)

	for len(args) > 0 {
		switch args[0] {
		case "-m", "--mbox":
			if len(args) < 2 {
				return fmt.Errorf("-m 需要指定 mbox 文件路径")
			}
			mboxFile = args[1]
			args = args[2:]
		case "-o", "--output":
			if len(args) < 2 {
				return fmt.Errorf("-o 需要指定输出路径")
			}
			output = args[1]
			args = args[2:]
		case "-v", "--revision":
			if len(args) < 2 {
				return fmt.Errorf("-v 需要指定版本号")
			}
			fmt.Sscanf(args[1], "%d", &revision)
			args = args[2:]
		case "-3", "--3way":
			threeWay = true
			args = args[1:]
		case "--add-link":
			addLink = true
			args = args[1:]
		case "--link-prefix":
			if len(args) < 2 {
				return fmt.Errorf("--link-prefix 需要指定前缀")
			}
			linkPrefix = args[1]
			args = args[2:]
		case "--add-message-id":
			addMsgID = true
			args = args[1:]
		case "--apply-cover-trailers":
			coverTrails = true
			args = args[1:]
		case "-h", "--help":
			printAMUsage()
			return nil
		default:
			// 最后一个参数可能是 mbox 文件路径
			if mboxFile == "" && !strings.HasPrefix(args[0], "-") {
				mboxFile = args[0]
				args = args[1:]
			} else {
				return fmt.Errorf("未知选项: %s", args[0])
			}
		}
	}

	_ = threeWay // 将在 shazam 中使用

	// 读取输入
	var reader io.Reader
	if mboxFile == "" || mboxFile == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(mboxFile)
		if err != nil {
			return fmt.Errorf("打开 mbox 文件: %w", err)
		}
		defer f.Close()
		reader = f
	}

	// 解析 mbox
	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(reader); err != nil {
		return fmt.Errorf("解析 mbox: %w", err)
	}

	// 获取指定版本的补丁系列
	series := mb.GetSeries(revision)
	if series == nil {
		return fmt.Errorf("找不到补丁系列 (版本 %d)", revision)
	}

	if !series.Complete {
		fmt.Fprintf(os.Stderr, "警告: 补丁系列不完整 (期望 %d, 找到 %d)\n",
			series.Expected, len(series.Patches))
	}

	// 生成 AM 可用的 mbox
	opts := patchwork.AMReadyOptions{
		AddLink:            addLink,
		LinkPrefix:         linkPrefix,
		AddMessageID:       addMsgID,
		ApplyCoverTrailers: coverTrails,
	}

	data, err := series.GetAMReady(opts)
	if err != nil {
		return fmt.Errorf("生成 AM 补丁: %w", err)
	}

	// 输出
	if output == "" || output == "-" {
		os.Stdout.Write(data)
	} else {
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("写入文件: %w", err)
		}
		fmt.Fprintf(os.Stderr, "已保存到 %s (%d 个补丁)\n", output, len(series.Patches))
	}

	return nil
}

func printAMUsage() {
	fmt.Println(`emx-b4 am - 从 mbox 创建 git-am 可用的补丁

用法:
  emx-b4 am [-m mbox_file] [-o output] [-v revision] [options]

选项:
  -m, --mbox FILE         输入 mbox 文件 (默认: stdin)
  -o, --output FILE       输出文件 (默认: stdout)
  -v, --revision N        选择补丁版本 (默认: 最新)
  -3, --3way              启用三路合并
  --add-link              添加 Link: trailer
  --link-prefix PREFIX    Link URL 前缀
  --add-message-id        添加 Message-Id trailer
  --apply-cover-trailers  将封面信的 trailer 应用到所有补丁`)
}

// ──────────────────────────────────────────────
// shazam: 直接应用补丁
// ──────────────────────────────────────────────

func cmdShazam(args []string) error {
	var (
		mboxFile string
		revision int
		threeWay bool
	)

	for len(args) > 0 {
		switch args[0] {
		case "-m", "--mbox":
			if len(args) < 2 {
				return fmt.Errorf("-m 需要指定 mbox 文件路径")
			}
			mboxFile = args[1]
			args = args[2:]
		case "-v", "--revision":
			if len(args) < 2 {
				return fmt.Errorf("-v 需要指定版本号")
			}
			fmt.Sscanf(args[1], "%d", &revision)
			args = args[2:]
		case "-3", "--3way":
			threeWay = true
			args = args[1:]
		case "-h", "--help":
			printShazamUsage()
			return nil
		default:
			if mboxFile == "" && !strings.HasPrefix(args[0], "-") {
				mboxFile = args[0]
				args = args[1:]
			} else {
				return fmt.Errorf("未知选项: %s", args[0])
			}
		}
	}

	// 读取输入
	var reader io.Reader
	if mboxFile == "" || mboxFile == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(mboxFile)
		if err != nil {
			return fmt.Errorf("打开 mbox 文件: %w", err)
		}
		defer f.Close()
		reader = f
	}

	// 解析 mbox
	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(reader); err != nil {
		return fmt.Errorf("解析 mbox: %w", err)
	}

	series := mb.GetSeries(revision)
	if series == nil {
		return fmt.Errorf("找不到补丁系列 (版本 %d)", revision)
	}

	// 生成 AM 可用的 mbox 数据
	opts := patchwork.AMReadyOptions{
		ApplyCoverTrailers: true,
	}
	data, err := series.GetAMReady(opts)
	if err != nil {
		return fmt.Errorf("生成 AM 补丁: %w", err)
	}

	// 使用 git am 应用补丁
	git := patchwork.NewGit(".")
	if !git.IsRepo() {
		return fmt.Errorf("当前目录不是 git 仓库")
	}

	fmt.Fprintf(os.Stderr, "正在应用 %d 个补丁...\n", len(series.Patches))

	if err := git.AMFromBytes(data, threeWay); err != nil {
		return fmt.Errorf("应用补丁失败: %w\n提示: 使用 'git am --abort' 取消", err)
	}

	fmt.Fprintf(os.Stderr, "成功应用 %d 个补丁\n", len(series.Patches))
	return nil
}

func printShazamUsage() {
	fmt.Println(`emx-b4 shazam - 直接将补丁应用到当前仓库

用法:
  emx-b4 shazam [-m mbox_file] [-v revision] [options]

选项:
  -m, --mbox FILE     输入 mbox 文件 (默认: stdin)
  -v, --revision N    选择补丁版本 (默认: 最新)
  -3, --3way          启用三路合并`)
}

// ──────────────────────────────────────────────
// prep: 准备补丁系列
// ──────────────────────────────────────────────

func cmdPrep(args []string) error {
	if len(args) == 0 {
		printPrepUsage()
		return nil
	}

	switch args[0] {
	case "new":
		return cmdPrepNew(args[1:])
	case "cover":
		return cmdPrepCover(args[1:])
	case "reroll":
		return cmdPrepReroll(args[1:])
	case "patches":
		return cmdPrepPatches(args[1:])
	case "status":
		return cmdPrepStatus(args[1:])
	case "list":
		return cmdPrepList(args[1:])
	default:
		return fmt.Errorf("未知的 prep 子命令: %s", args[0])
	}
}

func printPrepUsage() {
	fmt.Println(`emx-b4 prep - 准备补丁系列

用法:
  emx-b4 prep <subcommand> [options]

子命令:
  new     创建新的补丁分支
  cover   编辑封面信
  reroll  升级版本号
  patches 生成补丁文件
  status  显示当前状态
  list    列出所有补丁分支`)
}

func cmdPrepNew(args []string) error {
	var slug, baseBranch string

	for len(args) > 0 {
		switch args[0] {
		case "-n", "--name":
			if len(args) < 2 {
				return fmt.Errorf("-n 需要指定名称")
			}
			slug = args[1]
			args = args[2:]
		case "-b", "--base":
			if len(args) < 2 {
				return fmt.Errorf("-b 需要指定基础分支")
			}
			baseBranch = args[1]
			args = args[2:]
		default:
			if slug == "" && !strings.HasPrefix(args[0], "-") {
				slug = args[0]
				args = args[1:]
			} else {
				return fmt.Errorf("未知选项: %s", args[0])
			}
		}
	}

	if slug == "" {
		return fmt.Errorf("请指定分支名称: emx-b4 prep new <name>")
	}

	git := patchwork.NewGit(".")
	pb, err := patchwork.NewPrepBranch(git, slug, baseBranch)
	if err != nil {
		return err
	}

	if err := pb.Create(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "已创建补丁分支: %s\n", pb.BranchName())
	return nil
}

func cmdPrepCover(args []string) error {
	var subject, body string

	for len(args) > 0 {
		switch args[0] {
		case "-s", "--subject":
			if len(args) < 2 {
				return fmt.Errorf("-s 需要指定主题")
			}
			subject = args[1]
			args = args[2:]
		case "-b", "--body":
			if len(args) < 2 {
				return fmt.Errorf("-b 需要指定正文")
			}
			body = args[1]
			args = args[2:]
		default:
			return fmt.Errorf("未知选项: %s", args[0])
		}
	}

	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	if subject == "" {
		subject = pb.CoverSubject
	}

	if err := pb.SaveCover(subject, body); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "封面信已保存\n")
	return nil
}

func cmdPrepReroll(args []string) error {
	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	oldRev := pb.Revision
	if err := pb.Reroll(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "版本已升级: v%d → v%d\n", oldRev, pb.Revision)
	return nil
}

func cmdPrepPatches(args []string) error {
	var outputDir string

	for len(args) > 0 {
		switch args[0] {
		case "-o", "--output":
			if len(args) < 2 {
				return fmt.Errorf("-o 需要指定输出目录")
			}
			outputDir = args[1]
			args = args[2:]
		default:
			return fmt.Errorf("未知选项: %s", args[0])
		}
	}

	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	paths, err := pb.GetPatches(outputDir)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "已生成 %d 个补丁:\n", len(paths))
	for _, p := range paths {
		fmt.Println(p)
	}
	return nil
}

func cmdPrepStatus(args []string) error {
	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	fmt.Printf("分支:     %s\n", pb.BranchName())
	fmt.Printf("版本:     v%d\n", pb.Revision)
	fmt.Printf("基础分支: %s\n", pb.BaseBranch)
	if pb.ChangeID != "" {
		fmt.Printf("Change-ID: %s\n", pb.ChangeID)
	}
	if pb.CoverSubject != "" {
		fmt.Printf("封面主题: %s\n", pb.CoverSubject)
	}

	// 列出提交
	commits, err := pb.EnumerateCommits()
	if err == nil && len(commits) > 0 {
		fmt.Printf("\n提交 (%d):\n", len(commits))
		for i, c := range commits {
			fmt.Printf("  %d. %s\n", i+1, c)
		}
	}

	// Diffstat
	stat, err := pb.DiffStat()
	if err == nil && stat != "" {
		fmt.Printf("\n变更统计:\n%s", stat)
	}

	return nil
}

func cmdPrepList(args []string) error {
	git := patchwork.NewGit(".")
	branches, err := patchwork.ListPrepBranches(git)
	if err != nil {
		return err
	}

	if len(branches) == 0 {
		fmt.Println("没有补丁分支")
		return nil
	}

	for _, b := range branches {
		fmt.Println(b)
	}
	return nil
}

// ──────────────────────────────────────────────
// diff: 比较补丁版本
// ──────────────────────────────────────────────

func cmdDiff(args []string) error {
	var mboxFile string
	var rev1, rev2 int

	for len(args) > 0 {
		switch args[0] {
		case "-m", "--mbox":
			if len(args) < 2 {
				return fmt.Errorf("-m 需要指定 mbox 文件路径")
			}
			mboxFile = args[1]
			args = args[2:]
		case "-r", "--range":
			if len(args) < 2 {
				return fmt.Errorf("-r 需要指定版本范围 (如: 1..2)")
			}
			rangeStr := args[1]
			parts := strings.SplitN(rangeStr, "..", 2)
			if len(parts) != 2 {
				return fmt.Errorf("版本范围格式错误, 应为 N..M (如: 1..2)")
			}
			fmt.Sscanf(parts[0], "%d", &rev1)
			fmt.Sscanf(parts[1], "%d", &rev2)
			args = args[2:]
		case "-h", "--help":
			printDiffUsage()
			return nil
		default:
			if mboxFile == "" && !strings.HasPrefix(args[0], "-") {
				mboxFile = args[0]
				args = args[1:]
			} else {
				return fmt.Errorf("未知选项: %s", args[0])
			}
		}
	}

	if mboxFile == "" {
		return fmt.Errorf("请指定 mbox 文件")
	}

	f, err := os.Open(mboxFile)
	if err != nil {
		return fmt.Errorf("打开 mbox 文件: %w", err)
	}
	defer f.Close()

	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(f); err != nil {
		return fmt.Errorf("解析 mbox: %w", err)
	}

	// 获取两个版本
	series1 := mb.GetSeries(rev1)
	series2 := mb.GetSeries(rev2)

	if series1 == nil {
		return fmt.Errorf("找不到版本 v%d", rev1)
	}
	if series2 == nil {
		return fmt.Errorf("找不到版本 v%d", rev2)
	}

	// 简单的逐补丁差异比较
	fmt.Printf("== 比较 v%d (%d 个补丁) vs v%d (%d 个补丁) ==\n\n",
		series1.Revision, len(series1.Patches),
		series2.Revision, len(series2.Patches))

	maxPatches := len(series1.Patches)
	if len(series2.Patches) > maxPatches {
		maxPatches = len(series2.Patches)
	}

	for i := 0; i < maxPatches; i++ {
		var subj1, subj2 string
		if i < len(series1.Patches) {
			subj1 = series1.Patches[i].Parsed.Subject
		} else {
			subj1 = "(不存在)"
		}
		if i < len(series2.Patches) {
			subj2 = series2.Patches[i].Parsed.Subject
		} else {
			subj2 = "(不存在)"
		}

		if subj1 != subj2 {
			fmt.Printf("补丁 %d: 变更\n  v%d: %s\n  v%d: %s\n\n",
				i+1, series1.Revision, subj1, series2.Revision, subj2)
		} else {
			fmt.Printf("补丁 %d: 相同 - %s\n", i+1, subj1)
		}
	}

	return nil
}

func printDiffUsage() {
	fmt.Println(`emx-b4 diff - 比较补丁系列版本

用法:
  emx-b4 diff [-m mbox_file] [-r N..M]

选项:
  -m, --mbox FILE    输入 mbox 文件
  -r, --range N..M   版本范围 (如: 1..2)`)
}

// ──────────────────────────────────────────────
// mbox: 显示 mbox 信息
// ──────────────────────────────────────────────

func cmdMbox(args []string) error {
	var mboxFile string

	for len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			printMboxUsage()
			return nil
		default:
			if mboxFile == "" && !strings.HasPrefix(args[0], "-") {
				mboxFile = args[0]
				args = args[1:]
			} else {
				return fmt.Errorf("未知选项: %s", args[0])
			}
		}
	}

	if mboxFile == "" {
		return fmt.Errorf("请指定 mbox 文件")
	}

	f, err := os.Open(mboxFile)
	if err != nil {
		return fmt.Errorf("打开 mbox 文件: %w", err)
	}
	defer f.Close()

	// 读取全部内容以支持多次解析
	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("读取文件: %w", err)
	}

	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("解析 mbox: %w", err)
	}

	fmt.Printf("消息总数: %d\n", len(mb.Messages))
	fmt.Printf("版本数:   %d\n", len(mb.Series))
	fmt.Printf("未分类:   %d\n\n", len(mb.Unknowns))

	for rev, series := range mb.Series {
		fmt.Printf("== 版本 v%d ==\n", rev)
		if series.CoverLetter != nil {
			fmt.Printf("  封面信: %s\n", series.CoverLetter.Parsed.Subject)
		}
		fmt.Printf("  补丁数: %d/%d\n", len(series.Patches), series.Expected)
		for i, p := range series.Patches {
			fmt.Printf("  [%d] %s\n", i+1, p.Parsed.Subject)
			if len(p.BodyParts.Trailers) > 0 {
				for _, t := range p.BodyParts.Trailers {
					fmt.Printf("      %s\n", t.String())
				}
			}
		}
		fmt.Println()
	}

	return nil
}

func printMboxUsage() {
	fmt.Println(`emx-b4 mbox - 显示 mbox 文件信息

用法:
  emx-b4 mbox <file>`)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// absPath returns the absolute path of a file, or the original path if resolution fails.
func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
