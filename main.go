package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"regexp"
	"net/http"
	"encoding/base64"
)

type KconfigSymbol struct {
	Name         string
	Type         string
	Default      string
	Prompt       string
	Description  string
	Dependencies []*KconfigSymbol
}

type KconfigTree struct {
	Roots map[string]*KconfigSymbol   // symbols that root a tree
	Symbols map[string]*KconfigSymbol // all symbols
}

func NewKconfigTree() *KconfigTree {
	return &KconfigTree{
		Roots:   make(map[string]*KconfigSymbol),
		Symbols: make(map[string]*KconfigSymbol),
	}
}

func (tree *KconfigTree) AddSymbol(name string) *KconfigSymbol {
	if symbol, exists := tree.Symbols[name]; exists {
		debugIOPrintf("%s symb exist\n", name)
		return symbol
	}
	debugIOPrintf("%s sym added\n", name)
	symbol := &KconfigSymbol{Name: name}
	tree.Symbols[name] = symbol
	tree.Roots[name] = symbol
	return symbol
}

func (tree *KconfigTree) AddDependency(parentName, childName string) {
	parent := tree.AddSymbol(parentName)
	child := tree.AddSymbol(childName)
	debugIOPrintf("add %s to %s\n", childName, parentName)
	parent.Dependencies = append(parent.Dependencies, child)
	if _, exists := tree.Roots[childName]; exists {
		debugIOPrintf("%s was root deleted\n", childName)
		delete(tree.Roots, childName)
	}
}

func parseKernelConfig(filePath string) (map[string]string, error) {
	configMap := make(map[string]string)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "CONFIG_") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimPrefix(parts[0], "CONFIG_")
				value := parts[1]
				configMap[key] = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return configMap, nil
}

func extractIdentifiers(expression string) []string {
	re := regexp.MustCompile(`[a-zA-Z_]\w*`)
	matches := re.FindAllString(expression, -1)
	return matches
}
func removeAfterChar(s string, char string) string {
	index := strings.Index(s, char)
	if index != -1 {
		return s[:index]
	}
	return s
}
func (tree *KconfigTree) ParseKconfigFile(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	debugIOPrintf("parse file %s\n", filePath)
	scanner := bufio.NewScanner(file)
	var currentSymbol *KconfigSymbol
	var currentParents []*KconfigSymbol
	var inHelpBlock bool
	helpIndentation := ""

	for scanner.Scan() {
		line := scanner.Text()
		debugIOPrintf("File '%s' - current line: %s\n", filePath, line)
		trimmedLine := removeAfterChar(strings.TrimSpace(line), "#")

		if inHelpBlock {
			debugIOPrintf("help line\n")
			if isIndentedLine(line, helpIndentation) || isEmptyOrWhitespace(line) {
				currentSymbol.Description += strings.TrimSpace(line) + "\n"
				continue
			} else {
				inHelpBlock = false
			}
		}

		if strings.HasPrefix(trimmedLine, "if") {
			debugIOPrintf("if line '%s'\n", trimmedLine)
			conditions := extractIdentifiers(trimmedLine[2:])
			for _, condition := range conditions {
				currentParents = append(currentParents, tree.AddSymbol(condition))
			}
		} else if strings.HasPrefix(trimmedLine, "endif") {
			currentParents = []*KconfigSymbol{}
		}

		if strings.HasPrefix(trimmedLine, "config") || strings.HasPrefix(trimmedLine, "menuconfig") {
			debugIOPrintf("config line '%s'\n", trimmedLine)
			fields := strings.Fields(trimmedLine)
			if len(fields) > 1 {
				currentSymbol = tree.AddSymbol(fields[1])
				if len(currentParents)>0 {
					for _, currentParent := range currentParents {
						debugIOPrintf("add dependency '%s', '%s'\n", currentParent.Name, currentSymbol.Name)
						tree.AddDependency(currentParent.Name, currentSymbol.Name)
					}
				}
			}
		}

		if strings.HasPrefix(trimmedLine, "depends on") && currentSymbol != nil {
			debugIOPrintf("depends on line '%s'\n", trimmedLine)
			for trimmedLine[len(trimmedLine)-1] == '\\' {
				debugIOPrintf("whiling '%s'\n", trimmedLine)
				trimmedLine = trimmedLine[:len(trimmedLine)-2]
				scanner.Scan()
				trimmedLine = trimmedLine + strings.TrimSpace(scanner.Text())
			}
			dependencies := extractIdentifiers(trimmedLine[len("depends on"):])
			for _, dep := range dependencies {
				if dep != "if" {
					dep = strings.TrimSpace(dep)
					debugIOPrintf("add dependency '%s', '%s'\n", dep, currentSymbol.Name)
					tree.AddDependency(dep, currentSymbol.Name)
				}
			}
		}

		if strings.HasPrefix(trimmedLine, "select") && currentSymbol != nil {
			debugIOPrintf("select line '%s'\n", trimmedLine)
			for trimmedLine[len(trimmedLine)-1] == '\\' {
				debugIOPrintf("whiling '%s'\n", trimmedLine)
				trimmedLine = trimmedLine[:len(trimmedLine)-2]
				scanner.Scan()
				trimmedLine = trimmedLine + strings.TrimSpace(scanner.Text())
			}
			selections := extractIdentifiers(trimmedLine[len("select"):])
			for _, sel := range selections {
				if sel != "if" {
					sel = strings.TrimSpace(sel)
					debugIOPrintf("add dependency '%s', '%s'\n", currentSymbol.Name, sel)
					tree.AddDependency(currentSymbol.Name, sel)
				}
			}
		}

		if strings.HasPrefix(trimmedLine, "default") && currentSymbol != nil {
			currentSymbol.Default = strings.TrimSpace(trimmedLine[len("default"):])
		}

		if strings.HasPrefix(trimmedLine, "imply") && currentSymbol != nil {
			debugIOPrintf("imply line '%s'\n", trimmedLine)
			implies := strings.Split(trimmedLine[len("imply"):], "&&")
			for _, imp := range implies {
				imp = strings.TrimSpace(imp)
				debugIOPrintf("add dependency '%s', '%s'\n", imp, currentSymbol.Name)
				tree.AddDependency(imp, currentSymbol.Name)
			}
		}

		if strings.HasPrefix(trimmedLine, "prompt") && currentSymbol != nil {
			currentSymbol.Prompt = strings.TrimSpace(trimmedLine[len("prompt"):])
		}

		if currentSymbol != nil && (strings.HasPrefix(trimmedLine, "bool") || strings.HasPrefix(trimmedLine, "tristate") ||
			strings.HasPrefix(trimmedLine, "int") || strings.HasPrefix(trimmedLine, "hex") || strings.HasPrefix(trimmedLine, "string")) {
			currentSymbol.Type = trimmedLine
		}

		if currentSymbol != nil && strings.HasPrefix(trimmedLine, "help") {
			inHelpBlock = true
			helpIndentation = getIndentation(line)
			currentSymbol.Description = ""
		}

		if strings.HasPrefix(trimmedLine, "endmenu") || strings.HasPrefix(trimmedLine, "endchoice") ||
			strings.HasPrefix(trimmedLine, "choice") || strings.HasPrefix(trimmedLine, "source") {
			continue
		}

		if !strings.HasPrefix(trimmedLine, "config") && !strings.HasPrefix(trimmedLine, "menuconfig") &&
			!strings.HasPrefix(trimmedLine, "depends on") && !strings.HasPrefix(trimmedLine, "select") &&
			!strings.HasPrefix(trimmedLine, "default") && !strings.HasPrefix(trimmedLine, "imply") &&
			!strings.HasPrefix(trimmedLine, "prompt") &&
			!strings.HasPrefix(trimmedLine, "bool") && !strings.HasPrefix(trimmedLine, "tristate") &&
			!strings.HasPrefix(trimmedLine, "int") && !strings.HasPrefix(trimmedLine, "hex") &&
			!strings.HasPrefix(trimmedLine, "string") && !strings.HasPrefix(trimmedLine, "help") &&
			!strings.HasPrefix(trimmedLine, "if") && !strings.HasPrefix(trimmedLine, "endif") &&
			!strings.HasPrefix(trimmedLine, "endmenu") && !strings.HasPrefix(trimmedLine, "endchoice") &&
			!strings.HasPrefix(trimmedLine, "choice") && !strings.HasPrefix(trimmedLine, "source") &&
			!inHelpBlock {
			debugIOPrintf("Warning: Unrecognized token in %s: %s\n", filePath, trimmedLine)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading file %s: %v\n", filePath, err)
	}
}
func getIndentation(line string) string {
        for i := 0; i < len(line); i++ {
                if line[i] != ' ' && line[i] != '\t' {
                        return line[:i]
                }
        }
        return ""
}

func isIndentedLine(line, helpIndentation string) bool {
        return strings.HasPrefix(line, helpIndentation)
}

func isEmptyOrWhitespace(line string) bool {
        return strings.TrimSpace(line) == ""
}

func (tree *KconfigTree) ParseKconfigDir(rootDir string) {
	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		debugIOPrintf("Processing %s\n", path)
		iskcfile, _ := regexp.MatchString("Kconfig.*", filepath.Base(path))
		if !info.IsDir() && iskcfile {
			tree.ParseKconfigFile(path)
		}
		return nil
	})
}

func (tree *KconfigTree) PrintDot(k map[string]string) string {
	var processed []*KconfigSymbol
	var res string

	content := make(map[string]int)
	debugIOPrintf("# root list has %d symbs\n", len(tree.Roots))

	res = res + fmt.Sprintln("digraph G {")
	for _, root := range tree.Roots {
		debugIOPrintf("# root sym '%s'\n", root.Name)
		tree.printSymbol(root, k, &processed, &content)
	}
	for key, _ := range content {
		res = res + key
	}
	res = res + fmt.Sprintln("}")
	return res
}

func contains(s []*KconfigSymbol, e *KconfigSymbol) bool {
    for _, a := range s {
        if a == e {
            return true
        }
    }
    return false
}

func bothExist(a,b string, k map[string]string)bool{
	_, ex1 := k[a]
	_, ex2 := k[b]

	debugIOPrintf("# checking %s && %s ==> %b\n", a, b, ex1 && ex2)
	return ex1 && ex2
}
func (tree *KconfigTree) printSymbol(symbol *KconfigSymbol, k map[string]string, processed *([]*KconfigSymbol), res *map[string] int) {

	debugIOPrintf("# %s has %d symbs\n", symbol.Name, len(symbol.Dependencies))
	debugIOPrintf("# processed size %d symbs\n", len(*processed))
	*processed = append(*processed, symbol)
	for _, dep := range symbol.Dependencies {
		if bothExist(symbol.Name, dep.Name, k) {
			(*res)[fmt.Sprintf("  \"%s\" -> \"%s\";\n", symbol.Name, dep.Name)] = 1
		}
		if contains(*processed, dep) {
			debugIOPrintf("# dep %s is skipped\n", dep.Name)
		} else {
			tree.printSymbol(dep, k, processed, res)
		}
	}
}

func main() {
        debugIOPrintf("Preliminaries\n")

	dirPtr := flag.String("d", "", "Specify the kernel directory")
	filePtr := flag.String("c", "", "Specify the config file to verify")
	flag.Parse()

	if *dirPtr == "" {
		fmt.Println("Error: Please specify the kernel directory using the -d flag.")
		os.Exit(1)
	}

	if *filePtr == "" {
		fmt.Println("Error: Please specify a config file using the -c flag.")
		os.Exit(1)
	}

	dirInfo, err := os.Stat(*dirPtr)
	if err != nil {
		fmt.Printf("Error: The directory '%s' does not exist.\n", *dirPtr)
		os.Exit(1)
	}

	if !dirInfo.IsDir() {
		fmt.Printf("Error: '%s' is not a directory.\n", *dirPtr)
		os.Exit(1)
	}

	maintainersPath := filepath.Join(*dirPtr, "MAINTAINERS")
	if _, err := os.Stat(maintainersPath); os.IsNotExist(err) {
		fmt.Printf("Error: 'MAINTAINERS' file not found in the directory '%s'.\n", *dirPtr)
		os.Exit(1)
	}

	if err := os.Chdir(*dirPtr); err != nil {
		fmt.Printf("Error: Failed to change directory to '%s'.\n", *dirPtr)
		os.Exit(1)
	}

	if _, err := os.Stat(*filePtr); os.IsNotExist(err) {
		fmt.Printf("Error: The file '%s' does not exist.\n", *filePtr)
		os.Exit(1)
	}

	debugIOPrintf("Start\n")
	kconfigTree := NewKconfigTree()

	rootDir := "."

	debugIOPrintf("fetch symbols\n")
	kconfigTree.ParseKconfigDir(rootDir)
	debugIOPrintf("print diagram\n")
	k, e :=parseKernelConfig(*filePtr)
	if e!=nil {
		panic("")
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
		page, err := base64.StdEncoding.DecodeString(index)
		if err != nil {
			panic("fatal")
		}
		fmt.Fprintf(w,string(page[:]))
	})
	http.HandleFunc("/kconfigMap.dot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		s := kconfigTree.PrintDot(k)
		fmt.Fprintf(w,s)
	})
	fmt.Println("Server is running on http://localhost:8080")
	err = http.ListenAndServe(":8080", nil)
		if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
