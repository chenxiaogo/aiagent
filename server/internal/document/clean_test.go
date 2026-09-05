package document

import "testing"

func TestCleanText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "压平连续空行与行尾空白",
			in:   "第一行   \n\n\n\n第二行\t\t",
			want: "第一行\n\n第二行",
		},
		{
			name: "去掉分隔线",
			in:   "标题\n----------\n正文\n==========\n结尾",
			want: "标题\n正文\n结尾",
		},
		{
			name: "剔除全文反复出现的页眉页脚",
			in:   "公司机密\n第一段正文内容\n公司机密\n第二段正文内容\n公司机密\n第三段正文内容",
			want: "第一段正文内容\n第二段正文内容\n第三段正文内容",
		},
		{
			name: "连续相同短行只保留一次（HTML 导航残留）",
			in:   "导航项\n导航项\n正文内容",
			want: "导航项\n正文内容",
		},
		{
			// BOM 与零宽字符必须写成转义：直接落在源码里 Go 编译器会报 illegal byte order mark
			name: "去掉零宽字符与 BOM",
			in:   "\uFEFF带\u200B零宽\u200D的正文",
			want: "带零宽的正文",
		},
		{
			name: "去掉残留 HTML 标签与 script",
			in:   "<p>正文</p><script>var a=1;</script>",
			want: "正文",
		},
		{
			name: "长行重复不折叠（避免误删表格数据）",
			in:   "这是一段超过四十个字的相同内容用于验证长行不会被折叠这是一段超过四十个字的相同内容用于验证长行不会被折叠\n这是一段超过四十个字的相同内容用于验证长行不会被折叠这是一段超过四十个字的相同内容用于验证长行不会被折叠",
			want: "这是一段超过四十个字的相同内容用于验证长行不会被折叠这是一段超过四十个字的相同内容用于验证长行不会被折叠\n这是一段超过四十个字的相同内容用于验证长行不会被折叠这是一段超过四十个字的相同内容用于验证长行不会被折叠",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CleanText(c.in); got != c.want {
				t.Errorf("CleanText()\n got: %q\nwant: %q", got, c.want)
			}
		})
	}
}

func TestIsLowQuality(t *testing.T) {
	if !IsLowQuality("第 3 页") {
		t.Error("孤立页码应判为噪声")
	}
	if !IsLowQuality("---") {
		t.Error("纯符号应判为噪声")
	}
	if IsLowQuality("这是一段可用于检索的正文内容，包含有效信息。") {
		t.Error("正常正文不应判为噪声")
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint("同一段  内容\n换行不影响")
	b := Fingerprint("同一段内容换行不影响")
	if a != b {
		t.Errorf("指纹应忽略空白差异: %q vs %q", a, b)
	}
	if Fingerprint("另一段完全不同的内容") == a {
		t.Error("不同内容不应产生相同指纹")
	}
}
