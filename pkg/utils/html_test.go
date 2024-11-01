package utils

import (
	"fmt"
	"testing"
)

func TestSimplifyHTML_ComplexTable(t *testing.T) {
	input := `<h2 style="margin-top: 0pt; margin-bottom: 0pt; text-align: center; line-height: 28pt; break-after: avoid; font-family: Arial; font-size: 16pt;">竞争性谈判公告</h2>
<table style="border-collapse: collapse; border: none; font-family: 'Times New Roman' ; font-size: 10pt;" border="1" cellspacing="0">
<tbody>
<tr>
<td style="width: 43.1500pt;"><span style="font-family: 宋体;">5</span></td>
<td style="width: 202.9000pt;"><span style="font-family: 宋体;">台式机</span></td>
<td style="width: 45.3000pt;"><span style="font-family: 宋体;">台</span></td>
<td style="width: 37.1000pt;"><span style="font-family: 宋体;">1</span></td>
<td style="width: 33.5500pt;"> </td>
</tr>
<tr>
<td><span style="font-family: 宋体;">6</span></td>
<td><span style="font-family: 宋体;">车牌识别一体机（含机头）</span></td>
<td><span style="font-family: 宋体;">台</span></td>
<td><span style="font-family: 宋体;">2</span></td>
<td> </td>
</tr>
</tbody>
</table>
<p style="text-indent: 28pt;"><span style="font-family: 宋体;">2024年11月01日</span></p>`

	expected := `<b>竞争性谈判公告</b>
5,台式机,台,1,
6,车牌识别一体机（含机头）,台,2,
2024年11月01日`

	result := SimplifyHTML(input)
	if result != expected {
		t.Errorf("SimplifyHTML() failed\nExpected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSimplifyHTML_UnderlineText(t *testing.T) {
	input := `<span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;">2024</span>年<span style="text-decoration: underline;">11</span>月<span style="text-decoration: underline;">01</span>日`

	expected := `<u>2024</u>年<u>11</u>月<u>01</u>日`

	result := SimplifyHTML(input)
	if result != expected {
		t.Errorf("SimplifyHTML() failed\nExpected:\n%s\nGot:\n%s", expected, result)
	}
}

func Test_SimplifyHTML_TableWithoutBorder(t *testing.T) {
	input := `<h2 style="margin-top: 0pt; margin-bottom: 0pt; text-align: center; line-height: 28pt; break-after: avoid; font-family:
  Arial; font-size: 16pt;" align="center"><span style="font-family: 方正小标宋简体; font-size: 22.0000pt;"><span
      style="font-family: 方正小标宋简体;">竞争性谈判</span><span style="font-family: 方正小标宋简体;">公告</span></span></h2>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">我部就以下项目进行国内竞争性谈判采购，采购资金已全部落实，欢迎符合条件的供应商参加谈判。</span></span></p>\n<p style="text-indent:
  28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family:
      黑体;">一、项目名称：</span></span><u><span style="font-family: 黑体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 黑体;">车场信息化改造（</span></span></u><u><span style="font-family: 黑体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: 黑体;">四</span></span></u><u><span
      style="font-family: 黑体; text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        黑体;">次）</span></span></u></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
  text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 黑体; font-size:
    14.0000pt;"><span style="font-family: 黑体;">二、项目编号：</span></span><u><span style="font-family: 黑体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">2024-JLBBDC-W3002</span></span></u></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt
  0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family:
    黑体; font-size: 14.0000pt;"><span style="font-family: 黑体;">三、项目概况：</span></span></p>\n<div align="center">\n
  <table style="border-collapse: collapse; border: none; font-family: 'Times New Roman' ; font-size: 10pt;"
    border="1" cellspacing="0">\n<tbody>\n<tr style="height: 36.0500pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">序号</span></span></p>\n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: 1.0000pt solid windowtext;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; margin: 0pt 0pt
            0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">物资名称</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: 1.0000pt solid windowtext; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">计量</span></span></p>
          \n<p style="text-align: center; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size:
            10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">单位</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: 1.0000pt solid windowtext;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; margin: 0pt 0pt
            0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">数量</span></span></p>\n</td>\n<td style="width: 46.3500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: 1.0000pt solid windowtext; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">交货</span></span></p>
          \n<p style="text-align: center; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size:
            10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">时间</span></span></p>\n</td>\n<td style="width: 49.3500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: 1.0000pt solid windowtext;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; margin: 0pt 0pt
            0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">交货</span></span></p>\n<p style="text-align: center; margin:
            0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">地点</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: 1.0000pt solid windowtext; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">备注</span></span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n
        <td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">一</span></span></p>\n</td>\n<td style="width: 285.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" colspan="3" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">车辆管理系统</span></span></p>\n</td>\n<td style="width: 46.3500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" rowspan="24" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">合同签订之日起</span><span
                style="font-family: 宋体;">30天内全部交货并安装调试完毕</span></span></p>\n</td>\n<td style="width: 49.3500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" rowspan="24" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">辽宁省抚顺市清原满族自治县</span></span></p>\n</td>\n<td style="width: 33.5500pt; padding: 0.0000pt 5.4000pt
          0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: none;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt
            0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 202.9000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">车牌管理平台</span></span>
          </p>\n</td>\n<td style="width: 45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">套</span></span></p>\n</td>\n<td style="width: 37.1000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>
          \n</td>\n<td style="width: 33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: 1.0000pt solid windowtext; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">2</span></span></p>\n</td>\n<td style="width: 202.9000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">车辆管理服务器</span></span></p>\n</td>\n<td style="width: 45.3000pt; padding: 0.0000pt 5.4000pt
          0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">台</span></span></p>\n
        </td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">3</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">30位钥匙管理箱</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">4</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">拍摄设备（扫描仪、高拍仪等）</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">5</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">台式机</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">台</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">6</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">车牌识别一体机（含机头）</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">2</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">7</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">频读卡器（车辆进出通道方向各一个）</span></span></p>\n</td>\n<td
          style="width: 45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right:
          1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n
          <p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">4</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">8</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">车载模块</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">个</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">70</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">9</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">网络交换机</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">10</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">网线</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">箱</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">电源线</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">米</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">400</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">2</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">辅材</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">批</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">3</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">悬浮门</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: none;" valign="center">\n<p style="text-align: center; line-height: 28pt;
            margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family:
              宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">个</span></span></p>\n</td>\n<td
          style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right:
          1.0000pt solid windowtext; border-top: none; border-bottom: none;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>
          \n</td>\n<td style="width: 33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">二</span></span></p>\n</td>\n<td style="width: 285.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" colspan="3" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">安防设施</span></span></p>\n</td>\n<td style="width: 33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: none;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 202.9000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">监控摄像机（火点）</span></span></p>\n</td>\n<td style="width: 45.3000pt; padding: 0.0000pt 5.4000pt
          0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">台</span></span></p>\n
        </td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">16</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: 1.0000pt solid windowtext; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">2</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">报警器主机</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">个</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">2</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">3</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">视频监控主机</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">4</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">网络交换机</span></span></p>\n</td>\n<td style="width:
          45.3000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">台</span></span></p>\n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom:
          1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt
            0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width:
          33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid
          windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">5</span></span></p>
          \n</td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">网线</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">箱</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">2</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">6</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">电源线</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">米</span></span></p>
          \n</td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">400</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height:
        31.2000pt; page-break-inside: avoid;">\n<td style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt
          5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">7</span></span></p>\n
        </td>\n<td style="width: 202.9000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">辅材</span></span></p>\n</td>\n<td style="width: 45.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: none;" valign="center">\n<p style="text-align: center; line-height: 28pt;
            margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family:
              宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">批</span></span></p>\n</td>\n<td
          style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right:
          1.0000pt solid windowtext; border-top: none; border-bottom: none;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>
          \n</td>\n<td style="width: 33.5500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">三</span></span></p>\n</td>\n<td style="width: 285.3000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" colspan="3" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">设备安装调试服务</span></span></p>\n</td>\n<td style="width: 33.5500pt; padding: 0.0000pt 5.4000pt
          0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: none;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt
            0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr style="height: 31.2000pt; page-break-inside: avoid;">\n<td
          style="width: 43.1500pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid
          windowtext; border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid
          windowtext;" valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 202.9000pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align:
            center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;">
            <span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">设备安装调试服务</span></span></p>\n</td>\n<td style="width: 45.3000pt; padding: 0.0000pt 5.4000pt
          0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext; border-top: none;
          border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p style="text-align: center; line-height:
            28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">项</span></span></p>\n
        </td>\n<td style="width: 37.1000pt; padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none;
          border-right: 1.0000pt solid windowtext; border-top: none; border-bottom: 1.0000pt solid windowtext;"
          valign="center">\n<p style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
            font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">1</span></span></p>\n</td>\n<td style="width: 33.5500pt;
          padding: 0.0000pt 5.4000pt 0.0000pt 5.4000pt; border-left: none; border-right: 1.0000pt solid windowtext;
          border-top: 1.0000pt solid windowtext; border-bottom: 1.0000pt solid windowtext;" valign="center">\n<p
            style="text-align: center; line-height: 28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"> </span></p>\n</td>\n</tr>\n<tr
        style="height: 31.9000pt; page-break-inside: avoid;">\n<td style="width: 457.7000pt; padding: 0.0000pt
          5.4000pt 0.0000pt 5.4000pt; border-left: 1.0000pt solid windowtext; border-right: 1.0000pt solid windowtext;
          border-top: none; border-bottom: 1.0000pt solid windowtext;" colspan="7" valign="center">\n<p
            style="line-height: 114%; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">说明：</span></span></p>\n<p style="text-indent: 24pt; line-height: 114%; margin: 0pt 0pt 0.0001pt;
            text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
              font-size: 12.0000pt;">1.</span><span style="font-family: 宋体; font-size: 12.0000pt;"><span
                style="font-family: 宋体;">报价供应商应当</span></span><span style="font-family: 宋体; font-size:
              12.0000pt;"><span style="font-family: 宋体;">对所投包内所有产品和数量进行</span></span><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">唯一</span></span><span style="font-family: 宋体;
              font-size: 12.0000pt;"><span style="font-family: 宋体;">报价，否则视为无效</span></span><span style="font-family:
              宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">报价</span></span><span style="font-family:
              宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">。</span></span></p>\n<p style="text-indent:
            24pt; line-height: 114%; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;">2.</span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family:
                宋体;">报价应当包括所有物资供应、运输、安装调试、技术培训、售后服务、备品备件和伴随服务等价格。</span></span></p>\n<p style="text-indent: 24pt;
            line-height: 114%; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ;
            font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 12.0000pt;">3.</span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">报价</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">供应商</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">应当</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">保证所投</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">物资</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">为全新</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">且</span></span><span
              style="font-family: 宋体; font-size: 12.0000pt;"><span style="font-family: 宋体;">未使用过的产品。</span></span>
          </p>\n</td>\n</tr>\n</tbody>\n</table>\n</div>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt
  0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family:
    宋体; font-size: 14.0000pt;">1.本项目是否接受联合体</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">谈判：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">否</span></span></u><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">；</span></span></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;">2.项目预</span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">算：</span></span><u><span style="font-family: 宋体; text-decoration:
      underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">25万元</span></span></u><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">；</span></span></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
    14.0000pt;">3.最高限价：</span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">25万元</span></span></u><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">；</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt;
  margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 宋体; font-size: 14.0000pt;">4.本项目</span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">确定</span></span><u><span style="font-family: 宋体; text-decoration:
      underline; font-size: 14.0000pt;"> </span></u><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">1</span></span></u><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"> </span></u><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">家供应商成交。</span></span></p>\n<p style="text-indent: 28pt; line-height:
  28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family: 黑体;">四、报价供应商资格条件</span></span></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（一）具有企（事）业法人资格（有行业特殊情况的银行、保险、电力、电信等法人分支机构，会计师、律师等非法人组织，行业协会等社会团体法人除外）；</span></span>
</p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（二）国有企业；事业单位；军队单位；成立三年以上的非外资（含港澳台）独资或控股企业；</span></span></p>\n<p style="text-indent:
  28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">（三）具有良好的商业信誉和健全的财务会计制度；</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt
  0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">（四）具有履行合同所必需的设施设备、专业技术能力、质量保证体系和固定的生产经营、服务场地；</span></span>
</p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（五）有依法缴纳税收和社会保障资金的良好记录；</span></span></p>\n<p style="text-indent: 28pt; line-height:
  28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">（六）参加军队采购活动前</span><span
      style="font-family: 宋体;">3年内，在经营活动中没有受到刑事处罚或者责令停产停业、吊销许可证或者执照、较大数额罚款（200万元以上）等重大违法记录；</span></span></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（七）未被中国政府采购网（</span><span style="font-family:
      宋体;">www.ccgp.gov.cn）列入政府采购严重违法失信行为记录名单，未在军队采购网（www.plap.mil.cn）军队采购暂停名单处罚范围内或军队采购失信名单禁入处罚期和处罚范围内，以及未被“信用中国”（www.creditchina.gov.cn）列入严重失信主体名单或国家企业信用信息公</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">示系统（</span><span
      style="font-family: 宋体;">www.gsxt.gov.cn）列入严重违法失信名单（处罚期内）。</span></span></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">（八）本项目特定资格：</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">无</span></span></u><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">。</span></span></p>\n<p style="text-indent: 28.1pt; line-height:
  28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;">
  <strong><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
        宋体;">（</span></span></strong><strong><span style="font-family: 宋体; font-size: 14.0000pt;"><span
        style="font-family: 宋体;">九</span></span></strong><strong><span style="font-family: 宋体; font-size:
      14.0000pt;"><span style="font-family: 宋体;">）</span></span></strong><strong><span style="font-family: 宋体;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">有意愿参与该项目采购活动的供应商，必须登录供应商管理系统（互联网网址：</span><span
        style="font-family:
        宋体;">plap.mil.cn）进行注册。依托电子招投标系统实施的电子化项目，供应商必须完成注册，方可报名、获取采购文件。线下组织的非电子化项目，供应商可先行</span></span></strong><strong><span
      style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
        宋体;">获取采购文件，但必须在提交投标（报价）文件截止时间前完成注册，未完成的不得参加采购活动。</span></span></strong></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family:
      黑体;">五、谈判文件申领时间、地点、方式</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt
  0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">（一）</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">申领</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">时间</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">：</span></span><u><span style="font-family: 宋体; text-decoration:
      underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">2024</span></span></u><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">年</span></span><u><span
      style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        宋体;">11</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">月</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: 宋体;">01</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">日至</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">11</span></span></u><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">月</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">08</span></span></u><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">日</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">，每日</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">上午</span></span><u><span
      style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        宋体;">08</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;">:</span><u><span
      style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        宋体;">30</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">至</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: 宋体;">11</span></span></u><span style="font-family: 宋体; font-size:
    14.0000pt;">:</span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: 宋体;">30</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">，下午</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">13</span></span></u><span style="font-family: 宋体;
    font-size: 14.0000pt;">:</span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">30</span></span></u><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">至</span></span><u><span style="font-family: 宋体; text-decoration:
      underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">16</span></span></u><span style="font-family:
    宋体; font-size: 14.0000pt;">:</span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">30</span></span></u><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">。</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt;
  margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">（二）</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">申领地点</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">：</span></span><u><span
      style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        宋体;">沈阳市和平区三好街</span><span style="font-family: 宋体;">54号物产科贸大厦20楼2028室</span></span></u><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">。</span></span></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">三</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">）</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">申领谈判文件</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">时需提供以下</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">材料：</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt
  0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
    font-size: 14.0000pt;">1.</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">营业执照或事业单位法人证书复印件加盖公章</span></span><span style="font-family: 宋体; font-size: 14.0000pt;">(军队单位不需要提供)；</span>
</p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
    14.0000pt;">2.法定代表人资格证明书原件；</span></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
  text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
    14.0000pt;">3.法定代表人授权书原件，授权代表身份证和授权代表在</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">报价前</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;">4个月内</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">（不含报价当月）</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">连续</span>3个月由</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">报价供应商缴纳社保证明材料的复印件；</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt
  0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体;
    font-size: 14.0000pt;">4.非外资</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">独资</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">企业或控股企业的书面声明</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（事业单位、军队单位不需要提供）</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">；</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt;
  margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 宋体; font-size: 14.0000pt;">5.</span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">报价供应商</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">主要股东或出资人信息</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">；</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt;
  margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 宋体; font-size: 14.0000pt;">6.</span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">未被列入本公告第四条第（</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">七</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">）项明确的违法失信名单</span><span style="font-family:
      宋体;">的</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">承诺书。</span></span></p>\n<ul style="margin-top: 0px; margin-bottom: 0px;">\n<li class="MsoNormal"
    style="line-height: 28pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
        宋体;">申领</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
        宋体;">方式</span></span></li>\n</ul>\n<p style="margin-top: 0pt; margin-bottom: 0pt; text-indent: 27.8pt;
  line-height: 28pt; text-align: left; font-family: 宋体; font-size: 12pt;"><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">采取网上发售和</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">线下发售两种</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">方式。线下发售由投标供应商</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">携带申领</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">资料</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">至代理公司现地报名，线上报名由</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">投标人采取发送</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">电子邮件</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">方式递交报名资料，邮件主题：项目名称</span></span><span style="font-family: 宋体;
    font-size: 14.0000pt;">+</span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">项目编号</span></span><span style="font-family: 宋体; font-size: 14.0000pt;">+</span><span style="font-family:
    宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">公司名称；邮件内容：列明公司名称、法定代表人或授权代表人</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">姓名</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">及</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">联系方式；</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">邮件附件：需采用</span></span><span
    style="font-family: 宋体; font-size: 14.0000pt;">A4</span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">纸幅面，将报名材料加盖企业鲜章，按顺序制作成</span><span style="font-family:
      宋体;">1个PDF格式文件，文件名称与主题一致，复印件扫描无效。报名</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">材料</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">审核通过后，采购</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">机构联系人</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">向供应商邮箱发送招标文件电子版；审核未通过的，采购</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">机构联系人</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">以</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">邮件形式回复审核情况，供应</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">商</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">可在招标文件申领时间内重新提交材料。采购机构</span></span><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">或代理机构邮箱：</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        宋体;">yczb-024</span></span></u><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">@</span></span></u><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family:
        宋体;">yichengzhaobiao</span></span></u><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">.com</span></span></u><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">。</span></span></u></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">五</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">）</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">谈判</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">文件售价：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">200</span></span></u><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">元</span>/份，售后不退。</span></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family:
      黑体;">六、报</span></span><span style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family:
      黑体;">价</span></span><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span
      style="font-family: 黑体;">开始和截止时间及地点、方式</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt;
  margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">（一）报价</span></span><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family:
      宋体;">开始时间：</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: Times New Roman;">2024</span></span></u><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family:
      宋体;">年</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: Times New Roman;">11</span></span></u><span style="font-family: 'Times New Roman' ;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">月</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">18</span></span></u><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">日</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: Times New Roman;">09</span></span></u><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family:
      宋体;">时</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: Times New Roman;">00</span></span></u><span style="font-family: 'Times New Roman' ;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">分。</span></span></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">（二）报价</span></span><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">截止时间：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: Times New Roman;">2024</span></span></u><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family:
      宋体;">年</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: Times New Roman;">11</span></span></u><span style="font-family: 'Times New Roman' ;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">月</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">18</span></span></u><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">日</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: Times New Roman;">09</span></span></u><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family:
      宋体;">时</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: Times New Roman;">30</span></span></u><span style="font-family: 'Times New Roman' ;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">分。</span></span></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">（三）报价</span></span><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">地点：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">沈阳市和平区三好街</span><span style="font-family:
        宋体;">54号物产科贸大厦20楼</span></span></u><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">会议</span></span></u><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">室</span></span></u><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family: 宋体;">。</span></span>
</p>\n<p class="15" style="text-indent: 28.0000pt; line-height: 28.0000pt;"><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">（四）报价</span></span><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span style="font-family:
      宋体;">方式：</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">由报价供应商法定代表人或授权代表现场提</span></span><span style="font-family: 'Times New Roman' ; font-size:
    14.0000pt;"><span style="font-family: 宋体;">交</span></span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">报价文件</span></span><span style="font-family: 'Times New Roman' ;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">，不接受邮寄等其他方式。</span></span></p>\n<p style="text-indent:
  28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family:
      黑体;">七、谈判时间、地点</span></span></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt;
  text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">（一）谈判时间：</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">2024</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">年</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: Times New Roman;">11</span></span></u><span style="font-family: 'Times New Roman' ;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">月</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">18</span></span></u><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">日</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: Times New Roman;">09</span></span></u><span style="font-family: 宋体;
    font-size: 14.0000pt;"><span style="font-family: 宋体;">时</span></span><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">30</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">分</span></span><span style="font-family: 楷体; font-size: 14.0000pt;"><span style="font-family:
      楷体;">（应当</span></span><span style="font-family: 楷体; font-size: 14.0000pt;"><span style="font-family:
      楷体;">与报价截止时间保持一致）</span></span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">。</span></span></p>\n<p style="margin: 0pt 0pt 0.0001pt 27.4pt; line-height: 28pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">（二）谈判地点：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: 宋体;">沈阳市和平区三好街</span><span style="font-family:
        宋体;">54号物产科贸大厦20楼</span></span></u><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">会议</span></span></u><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">室</span></span></u><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">。</span></span></p>\n<p
  style="text-indent: 30.65pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 黑体; font-size: 14.0000pt;"><span
      style="font-family: 黑体;">八</span></span><span style="font-family: 黑体; font-size: 14.0000pt;"><span
      style="font-family: 黑体;">、</span></span><span style="font-family: 黑体; font-size: 14.0000pt;"><span
      style="font-family: 黑体;">本采购项目相关信息在《军队采购网》（</span></span><span style="font-family: 黑体; font-size:
    14.0000pt;"><span style="font-family: Times New Roman;">plap.mil.cn</span></span><span style="font-family: 黑体;
    font-size: 14.0000pt;"><span style="font-family: 黑体;">）</span></span><span style="font-family: 黑体; font-size:
    14.0000pt;"><span style="font-family: 黑体;">和</span></span><span style="font-family: 黑体; font-size:
    14.0000pt;"><span style="font-family: 黑体;">《中国政府采购网》（</span><span style="font-family: Times New
      Roman;">www.ccgp.gov.cn</span><span style="font-family:
      黑体;">）上发布，对于因其他网站转载并发布的非完整版或修改版公告，而导致误报名或未报名的情形，招标人及招标代理机构不予承担责任</span></span><span style="font-family: 黑体;
    font-size: 14.0000pt;"><span style="font-family: 黑体;">。</span></span></p>\n<p style="text-indent: 28pt;
  line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family: 黑体;">九</span></span><span
    style="font-family: 黑体; font-size: 14.0000pt;"><span style="font-family: 黑体;">、采购机构联系方式</span></span></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">联</span></span><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;">
  </span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">系</span></span><span
    style="font-family: 'Times New Roman' ; font-size: 14.0000pt;"> </span><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">人：</span></span><u><span style="font-family: 宋体; text-decoration:
      underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">马帅、李昂、丁艺、徐峰、英峰铭、赵昱博、代春雨、王秋明</span></span></u>
</p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">移动电话：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: Times New Roman;">17640025161</span></span></u></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">地</span></span><span style="font-family: 'Times New Roman' ; font-size: 14.0000pt;">
  </span><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">址：</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: 宋体;">沈阳市和平区三好街</span><span style="font-family: Times New Roman;">54</span><span
        style="font-family: 宋体;">号物产科贸大厦</span><span style="font-family: Times New Roman;">20</span><span
        style="font-family: 宋体;">楼</span><span style="font-family: Times New Roman;">2028</span><span
        style="font-family: 宋体;">室</span></span></u></p>\n<p style="text-indent: 28pt; line-height: 28pt; margin: 0pt
  0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family:
    黑体; font-size: 14.0000pt;"><span style="font-family: 黑体;">十、监督部门联系方式</span></span></p>\n<p style="text-indent:
  28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size:
  10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">项目监督人：</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size:
      14.0000pt;"><span style="font-family: 宋体;">卢</span></span></u><u><span style="font-family: 宋体;
      text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: 宋体;">先生</span></span></u><u><span
      style="font-family: 'Times New Roman' ; text-decoration: underline; font-size: 14.0000pt;"> </span></u></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><span style="font-family: 宋体; font-size: 14.0000pt;"><span
      style="font-family: 宋体;">移动电话：</span></span><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"><span style="font-family: Times New Roman;">18341956829</span></span></u></p>\n<p
  style="text-indent: 28pt; line-height: 28pt; margin: 0pt 0pt 0.0001pt; text-align: justify;
  font-family: 'Times New Roman' ; font-size: 10.5pt;"><u><span style="font-family: 宋体; text-decoration: underline;
      font-size: 14.0000pt;"> </span></u></p>\n<p style="text-indent: 28pt; text-align: right; line-height: 20pt;
  margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><em><span style="font-family: 宋体;
      font-style: italic; font-size: 14.0000pt;"><span style="font-family: 宋体;">采购机构：</span></span></em><em><u><span
        style="font-family: 宋体; text-decoration: underline; font-style: italic; font-size: 14.0000pt;"><span
          style="font-family: 宋体;">依诚招标有限公司</span></span></u></em></p>\n<p style="text-align: right; line-height:
  28pt; margin: 0pt 0pt 0.0001pt; font-family: 'Times New Roman' ; font-size: 10.5pt;"><u><span style="font-family:
      宋体; text-decoration: underline; font-size: 14.0000pt;"><span style="font-family: Times New
        Roman;">2024</span></span></u><span style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family:
      宋体;">年</span></span><u><span style="font-family: 宋体; text-decoration: underline; font-size: 14.0000pt;"><span
        style="font-family: Times New Roman;">11</span></span></u><span style="font-family: 宋体; font-size:
    14.0000pt;"><span style="font-family: 宋体;">月</span></span><u><span style="font-family: 宋体; text-decoration:
      underline; font-size: 14.0000pt;"><span style="font-family: Times New Roman;">01</span></span></u><span
    style="font-family: 宋体; font-size: 14.0000pt;"><span style="font-family: 宋体;">日</span></span></p>\n<p
  style="margin: 0pt 0pt 0.0001pt; text-align: justify; font-family: 'Times New Roman' ; font-size: 10.5pt;"><span
    style="font-family: 'Times New Roman' ; font-size: 10.5000pt;"> </span></p>`

	content := SimplifyHTML(input)
	fmt.Println(content)
}
