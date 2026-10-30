package research

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The layout follows the supplied two-column article example. All model text
// stays escaped; only this application-owned template emits LaTeX commands.
const manuscriptPreamble = `%% Kairo two-column conference-style research manuscript
\documentclass[10pt,twocolumn]{article}
\usepackage[margin=0.75in,top=0.9in,bottom=1in]{geometry}
\usepackage{iftex}
\ifPDFTeX
\usepackage[T1]{fontenc}
\usepackage[utf8]{inputenc}
\usepackage{lmodern}
\else
\usepackage{fontspec}
\setmainfont{Latin Modern Roman}
\fi
\usepackage{booktabs}
\usepackage{amsmath,amssymb}
\usepackage{graphicx}
\usepackage{listings}
\usepackage{xcolor}
\usepackage{multirow,array,subcaption}
\usepackage{microtype}
\usepackage{abstract}
\usepackage{titlesec,titling}
\usepackage{fancyhdr}
\usepackage[numbers,sort&compress]{natbib}
\usepackage{hyperref}
\hypersetup{hidelinks}
\renewcommand{\abstractnamefont}{\normalfont\bfseries}
\setlength{\absleftindent}{0pt}
\setlength{\absrightindent}{0pt}
\graphicspath{{./figures/}}
\pagestyle{fancy}
\fancyhf{}
\lhead{\small Kairo research manuscript}
\cfoot{\thepage}
\renewcommand{\headrulewidth}{0.4pt}
\setlength{\headheight}{14pt}
\setlength{\droptitle}{-2em}
\pretitle{\begin{center}\Large\bfseries}
\posttitle{\end{center}\vspace{-0.3em}}
\preauthor{\par\centering\small\lineskip0.75em}
\postauthor{\par\vspace{0.3em}}
\predate{}\postdate{}
\titleformat{\section}{\normalfont\large\bfseries}{\thesection}{0.5em}{}
\titleformat{\subsection}{\normalfont\normalsize\bfseries}{\thesubsection}{0.5em}{}
\setlength{\columnsep}{0.25in}
\setlength{\emergencystretch}{2em}
\makeatletter
\setlength{\@fptop}{0pt}
\setlength{\@fpsep}{12pt}
\setlength{\@fpbot}{0pt plus 1fil}
\makeatother
`

func renderManuscript(e Experiment, d documentSections, figures []DocumentFigure) string {
	var out strings.Builder
	out.WriteString(manuscriptPreamble)
	out.WriteString("\\title{" + latexText(d.Title) + "}\n")
	out.WriteString("% Replace these placeholders with the verified authors and affiliations.\n\\author{\\begin{tabular}[t]{c}\nAuthor names to be added \\\\[0.3em]\n\\small Affiliation to be added\n\\end{tabular}}\n\\date{}\n\\begin{document}\n\\twocolumn[{%\n\\begin{@twocolumnfalse}\n\\maketitle\n\\vspace{-0.5em}\n\\begin{abstract}\n" + latexText(d.Abstract) + "\n")
	keywords := d.Keywords
	if len(keywords) == 0 {
		keywords = []string{"computational research"}
		if e.Plan.Domain != "" {
			keywords = append(keywords, e.Plan.Domain)
		}
		keywords = append(keywords, planLanguage(*e.Plan))
	}
	out.WriteString("\n\\noindent\\textbf{Keywords:} " + latexText(strings.Join(keywords, ", ")) + "\n\\end{abstract}\n\\smallskip\n{\\small\\noindent\\textbf{Research draft.} Based on one recorded computational experiment. Review scientific claims, authorship, and references before sharing.}\n\\end{@twocolumnfalse}\n\\vspace{1.5em}\n}]\n\\pagestyle{fancy}\n\\thispagestyle{fancy}\n\\raggedbottom\n")
	background := d.Background
	if strings.TrimSpace(background) == "" {
		background = "This study addresses the recorded research question using the methodology described below. No verified external literature was supplied; authors should add a reviewed account of related work and appropriate citations."
	}
	setup := d.ExperimentalSetup
	if strings.TrimSpace(setup) == "" {
		setup = "The experimental setup uses the saved parameters, attached dataset when present, and runtime configuration of the recorded run. No additional trials or datasets are implied."
	}
	sections := [][3]string{{"Introduction", "intro", d.Introduction}, {"Background and Related Work", "background", background}, {"Methodology", "method", d.Methods}, {"Experimental Setup", "setup", setup}, {"Results", "results", d.Results}, {"Discussion", "discussion", d.Discussion}, {"Conclusion", "conclusion", d.Conclusion}}
	for _, section := range sections {
		out.WriteString("\n% =============================================================\n\\section{" + section[0] + "}\n\\label{sec:" + section[1] + "}\n" + latexText(section[2]) + "\n\n")
		switch section[0] {
		case "Experimental Setup":
			runtime, python := e.Result.RuntimeVersion, e.Result.PythonVersion
			if runtime == "" {
				runtime = "not recorded"
			}
			if python == "" {
				python = "not recorded"
			}
			out.WriteString("\\subsection{Recorded configuration}\nLanguage: " + latexText(planLanguage(*e.Plan)) + ". Runtime: " + latexText(runtime) + ". Python worker: " + latexText(python) + ".\n")
			if e.DatasetID != "" {
				out.WriteString("Dataset: " + latexText(e.DatasetID) + ".\n")
			}
			// Compact configuration values wrap within a column rather than overflowing
			// a verbatim block with JSON or long file/record identifiers.
			var parameters map[string]any
			if json.Unmarshal(e.Plan.Parameters, &parameters) == nil && len(parameters) > 0 {
				out.WriteString("\\paragraph{Parameters.}\n")
				for _, metric := range documentMetrics(parameters) {
					out.WriteString(latexColumnText(metric.Name, 64) + ": " + latexColumnText(metric.Value, 180) + ". ")
				}
				out.WriteString("\n")
			}
		case "Results":
			out.WriteString("\\subsection{Recorded metrics}\n")
			metrics := documentMetrics(e.Result.Metrics)
			if len(metrics) == 0 {
				metrics = []metricEntry{{"No metrics", "No numerical metrics recorded."}}
			}
			for start := 0; start < len(metrics); start += 8 {
				caption := "Computed metrics from the recorded experiment."
				if start > 0 {
					caption = "Computed metrics (continued)."
				}
				out.WriteString("\\begin{table}[htbp]\n\\centering\n\\caption{" + caption + "}\n\\label{tab:recorded-metrics-" + fmt.Sprint(start/8+1) + "}\n\\small\n\\begin{tabular}{@{}>{\\raggedright\\arraybackslash}p{0.35\\linewidth}>{\\raggedright\\arraybackslash}p{0.59\\linewidth}@{}}\n\\toprule\n\\textbf{Metric} & \\textbf{Recorded value} \\\\\n\\midrule\n")
				for _, metric := range metrics[start:min(start+8, len(metrics))] {
					out.WriteString(latexColumnText(metric.Name, 64) + " & " + latexColumnText(metric.Value, 160) + " \\\\\n")
				}
				out.WriteString("\\bottomrule\n\\end{tabular}\n\\end{table}\n")
			}
			out.WriteString("Metric displays are limited to 60 entries; long names and values are abbreviated. Complete recorded values are included in \\texttt{evidence.json}.\n")

			for i, figure := range figures {
				out.WriteString("\\begin{figure}[htbp]\n\\centering\n\\includegraphics[width=0.96\\linewidth]{" + figure.PDF + "}\n\\caption{" + latexText(figure.Caption) + "}\n\\label{fig:kairo-" + fmt.Sprint(i+1) + "}\n\\end{figure}\n")
			}
		case "Discussion":
			out.WriteString("\\subsection{Assumptions and limitations}\n\\begin{itemize}\n")
			limitations := append(append(append([]string{}, e.Plan.Assumptions...), e.Plan.Limitations...), e.Result.Limitations...)
			if len(limitations) == 0 {
				limitations = []string{"No explicit limitations were recorded. Author review is required."}
			}
			for _, text := range limitations {
				out.WriteString("\\item " + latexText(text) + "\n")
			}
			out.WriteString("\\end{itemize}\n")
		}
	}
	out.WriteString("\\section{Reproducibility}\n\\label{sec:reproducibility}\nExperiment: " + latexColumnText(e.ID, 200) + ". Attempt: " + fmt.Sprint(e.Attempt) + ". Plan version: " + fmt.Sprint(e.PlanVersion) + ". Use the saved experiment reproduction bundle for source code, exact inputs, dependencies, and runtime details.\n")
	out.WriteString("% Add only verified references; natbib supports numeric citations.\n% After adding bibliography entries, replace the placeholder References section\n% with a thebibliography environment or a verified .bib file.\n% \\bibliographystyle{ACM-Reference-Format}\n\\section*{References}\nNo external references were supplied. Authors must add and verify appropriate citations before publication.\n\\end{document}\n")
	return out.String()
}

// Break long identifiers safely without allowing user text to introduce macros.
func latexColumnText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		runes = append(runes[:limit], []rune(" [abbreviated]")...)
	}
	var out strings.Builder
	word := 0
	for _, char := range runes {
		out.WriteString(latexText(string(char)))
		if char == ' ' || char == '\n' {
			word = 0
		} else {
			word++
			if word == 16 {
				out.WriteString(`\allowbreak{}`)
				word = 0
			}
		}
	}
	return out.String()
}
