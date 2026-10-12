# PowerShell の整形と検査（docs/format.md・docs/lint.md）。scripts/powershell.ps1 が両方に渡す。
# 整形は PSScriptAnalyzer の CodeFormatting と同じ規則を、インデントだけタブ（.editorconfig）に
# 替えて使う。検査は既定の規則のまま（IncludeRules を書かない）で、整形の規則も指摘する。
@{
	Rules = @{
		PSPlaceOpenBrace           = @{
			Enable             = $true
			OnSameLine         = $true
			NewLineAfter       = $true
			IgnoreOneLineBlock = $true
		}
		PSPlaceCloseBrace          = @{
			Enable             = $true
			NewLineAfter       = $true
			IgnoreOneLineBlock = $true
			NoEmptyLineBefore  = $false
		}
		PSUseConsistentIndentation = @{
			Enable              = $true
			Kind                = 'tab'
			PipelineIndentation = 'IncreaseIndentationForFirstPipeline'
			IndentationSize     = 4
		}
		PSUseConsistentWhitespace  = @{
			Enable                                  = $true
			CheckInnerBrace                         = $true
			CheckOpenBrace                          = $true
			CheckOpenParen                          = $true
			CheckOperator                           = $true
			CheckPipe                               = $true
			CheckPipeForRedundantWhitespace         = $false
			CheckSeparator                          = $true
			CheckParameter                          = $false
			IgnoreAssignmentOperatorInsideHashTable = $true
		}
		PSAlignAssignmentStatement = @{
			Enable         = $true
			CheckHashtable = $true
		}
		PSUseCorrectCasing         = @{
			Enable = $true
		}
	}
}
