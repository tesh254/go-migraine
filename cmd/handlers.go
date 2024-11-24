package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tesh254/migraine/kv"
	"github.com/tesh254/migraine/run"
	"github.com/tesh254/migraine/utils"
	"github.com/tesh254/migraine/workflow"
)

func handleNewTemplate(templatePath string) error {
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template file: %v", err)
	}

	// Extract slug from filename
	slug := filepath.Base(templatePath)
	slug = slug[:len(slug)-len(filepath.Ext(slug))]
	slug = utils.FormatString(slug)

	// Initialize KV store
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		return fmt.Errorf("failed to initialize kv store: %v", err)
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	templateStore := kv.NewTemplateStoreManager(store)

	// Check if template exists
	existing, err := templateStore.GetTemplate(slug)
	if err == nil && existing != nil {
		return fmt.Errorf("template with name '%s' already exists", slug)
	}

	// Extract variables
	variables := utils.ExtractTemplateVars(string(content))
	if len(variables) > 0 {
		utils.LogInfo("Template variables detected:")
		for _, v := range variables {
			fmt.Printf("  • %s\n", v)
		}
		fmt.Println()
	}

	// Create template
	template := kv.TemplateItem{
		Slug:     slug,
		Workflow: string(content),
	}

	if err := templateStore.CreateTemplate(template); err != nil {
		return fmt.Errorf("failed to create template: %v", err)
	}

	utils.LogSuccess(fmt.Sprintf("Template '%s' created successfully", slug))
	return nil
}

func handleListTemplates() {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to initialize kv store: %v", err))
		return
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	templateStore := kv.NewTemplateStoreManager(store)

	templates, err := templateStore.ListTemplates()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to list templates: %v", err))
		return
	}

	if len(templates) == 0 {
		fmt.Println("No templates found")
		return
	}

	fmt.Println("\nAvailable templates:")
	for _, t := range templates {
		fmt.Printf("  • %s\n", t.Slug)
	}
}

func handleDeleteTemplate(templateName string) error {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		return fmt.Errorf("failed to initialize kv store: %v", err)
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	templateStore := kv.NewTemplateStoreManager(store)

	if err := templateStore.DeleteTemplate(templateName); err != nil {
		return fmt.Errorf("failed to delete template: %v", err)
	}

	utils.LogSuccess(fmt.Sprintf("Template '%s' deleted successfully", templateName))
	return nil
}

func handleAddWorkflow() {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to initialize kv store: %v", err))
		return
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	templateStore := kv.NewTemplateStoreManager(store)
	workflowStore := kv.NewWorkflowStore(store)

	templates, err := templateStore.ListTemplates()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to list templates: %v", err))
		os.Exit(1)
	}

	if len(templates) == 0 {
		utils.LogError(fmt.Sprintf("No template found. Fork or Create a template."))
		os.Exit(1)
	}

	fmt.Println("\nAvailable templates:")
	for i, t := range templates {
		fmt.Printf("[%d] %s\n", i+1, t.Slug)
	}

	// Template selection
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\nEnter template number: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to read input: %v", err))
		os.Exit(1)
	}

	// Parse template selection
	selection, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || selection < 1 || selection > len(templates) {
		utils.LogError("Invalid template selection")
		os.Exit(1)
	}

	selectedTemplate := templates[selection-1]

	// Parse template into workflow to access config
	parser := workflow.NewTemplateParser(selectedTemplate.Workflow)
	wk, err := parser.ParseToWorkflow()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to parse workflow template: %v", err))
		os.Exit(1)
	}

	// Get workflow name first
	fmt.Print("\nEnter workflow name: ")
	workflowName, err := reader.ReadString('\n')
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to read input: %v", err))
		os.Exit(1)
	}

	workflowName = strings.TrimSpace(workflowName)
	if workflowName == "" {
		utils.LogError("Workflow name cannot be empty")
		os.Exit(1)
	}

	slugifiedName := utils.FormatString(workflowName)

	// Initialize the workflow with basic information
	kvWorkflow := kv.Workflow{
		ID:          slugifiedName,
		Name:        wk.Name,
		PreChecks:   make([]kv.Atom, len(wk.PreChecks)),
		Steps:       make([]kv.Atom, len(wk.Steps)),
		Description: wk.Description,
		Actions:     make(map[string]kv.Atom),
		Config: kv.Config{
			Variables:      wk.Config.Variables,
			StoreVariables: wk.Config.StoreVariables,
		},
		UsesSudo: false,
	}

	// Handle variables based on StoreVariables config
	if wk.Config.StoreVariables {
		// Extract variables and get their values
		variables := utils.ExtractTemplateVars(selectedTemplate.Workflow)
		if len(variables) > 0 {
			fmt.Printf("\nEnter variables:\n")
			variableValues := make(map[string]string)

			for _, v := range variables {
				fmt.Printf("%s: ", v)
				value, err := reader.ReadString('\n')
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to read input: %v", err))
					os.Exit(1)
				}

				value = strings.TrimSpace(value)
				if value == "" {
					utils.LogError(fmt.Sprintf("Variable %s cannot be empty", v))
					os.Exit(1)
				}

				// Process variable based on config rules
				if rules, exists := wk.Config.Variables[v]; exists {
					if rulesArray, ok := rules.([]interface{}); ok {
						for _, rule := range rulesArray {
							if ruleStr, ok := rule.(string); ok {
								switch ruleStr {
								case "slugify":
									value = utils.FormatString(value)
								}
							}
						}
					}
				}

				variableValues[v] = value
			}

			// Process PreChecks with variables
			for i, step := range wk.PreChecks {
				command, err := utils.ApplyVariablesToCommand(step.Command, variableValues)
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to process step command: %v", err))
					os.Exit(1)
				}

				kvWorkflow.PreChecks[i] = kv.Atom{
					Command:     command,
					Description: step.Description,
				}
			}

			// Process Steps with variables
			for i, step := range wk.Steps {
				command, err := utils.ApplyVariablesToCommand(step.Command, variableValues)
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to process step command: %v", err))
					os.Exit(1)
				}

				kvWorkflow.Steps[i] = kv.Atom{
					Command:     command,
					Description: step.Description,
				}
			}

			// Process Actions with variables
			for key, action := range wk.Actions {
				command, err := utils.ApplyVariablesToCommand(action.Command, variableValues)
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to process action command: %v", err))
					os.Exit(1)
				}

				kvWorkflow.Actions[key] = kv.Atom{
					Command:     command,
					Description: action.Description,
				}
			}
		}
	} else {
		// If StoreVariables is false, copy steps and actions as is
		for i, step := range wk.Steps {
			kvWorkflow.Steps[i] = kv.Atom{
				Command:     step.Command,
				Description: step.Description,
			}
		}

		for key, action := range wk.Actions {
			kvWorkflow.Actions[key] = kv.Atom{
				Command:     action.Command,
				Description: action.Description,
			}
		}
	}

	// Create the workflow
	err = workflowStore.CreateWorkflow(slugifiedName, kvWorkflow)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to create workflow: %v", err))
		os.Exit(1)
	}

	utils.LogSuccess(fmt.Sprintf("Workflow '%s' created successfully", slugifiedName))
}

func handleListWorkflows() {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to initialize kv store: %v", err))
		return
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	workflowStore := kv.NewWorkflowStore(store)

	workflows, err := workflowStore.ListWorkflows()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to list workflows: %v", err))
		return
	}

	if len(workflows) == 0 {
		fmt.Println("\nNo workflows found")
		return
	}

	fmt.Printf("\n%sAvailable Workflows:%s\n\n", utils.BOLD, utils.RESET)

	for i, workflow := range workflows {
		// Print workflow name, ID (slug), and description
		fmt.Printf("%d. (%s%s%s) %s\n", i+1, utils.BOLD, workflow.ID, utils.RESET, workflow.Name)
		if workflow.Description != nil && *workflow.Description != "" {
			fmt.Printf("   Description: %s\n", *workflow.Description)
		}

		// Extract and display unique variables from steps and actions
		variables := make(map[string]bool)

		// Extract from steps
		for _, step := range workflow.Steps {
			vars := utils.ExtractTemplateVars(step.Command)
			for _, v := range vars {
				variables[v] = true
			}
		}

		// Extract from actions
		for _, action := range workflow.Actions {
			vars := utils.ExtractTemplateVars(action.Command)
			for _, v := range vars {
				variables[v] = true
			}
		}

		// Display variables if any exist
		if len(variables) > 0 {
			fmt.Printf("   Required Variables:\n")
			for v := range variables {
				fmt.Printf("   • %s\n", v)
			}
		}

		// Add a newline between workflows for better readability
		fmt.Println()
	}
}

func handleDeleteWorkflow(workflowId string) {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		utils.LogError(fmt.Sprintf("failed to initialize kv store: %v", err))
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	workflowStore := kv.NewWorkflowStore(store)

	if err := workflowStore.DeleteWorkflow(workflowId); err != nil {
		utils.LogError(fmt.Sprintf("failed to delete workflow: %v", err))
	}

	utils.LogSuccess(fmt.Sprintf("Workflow '%s' deleted successfully", workflowId))
}

func runPreChecks(workflow *kv.Workflow, variables map[string]string) error {
	if len(workflow.PreChecks) == 0 {
		return nil
	}

	fmt.Printf("\nRunning pre-checks for workflow: %s\n\n", workflow.Name)

	for i, check := range workflow.PreChecks {
		command, err := utils.ApplyVariablesToCommand(check.Command, variables)
		if err != nil {
			return fmt.Errorf("failed to process precheck command: %v", err)
		}

		utils.ColorSizePrint("blue", "small", fmt.Sprintf("pre_check [%d]: %s\n", i+1, *check.Description))
		utils.ColorSizePrint("blue", "small", fmt.Sprintf("$_: %s\n\n", command))

		if err := run.ExecuteCommand(command); err != nil {
			utils.LogError(fmt.Sprintf("precheck failed: %v", err))
			return fmt.Errorf("precheck failed")
		}

		fmt.Printf("%s PreCheck completed successfully %s\n\n", utils.CHECK, utils.CHECK)
	}

	return nil
}

func executeAction(workflow *kv.Workflow, actionName string, variables map[string]string) error {
	if len(workflow.PreChecks) > 0 {
		if err := runPreChecks(workflow, variables); err != nil {
			return fmt.Errorf("pre-checks failed: %v", err)
		}
	}

	action, exists := workflow.Actions[actionName]
	if !exists {
		return fmt.Errorf("action '%s' not found in workflow", actionName)
	}

	// Replace variables in command
	command, err := utils.ApplyVariablesToCommand(action.Command, variables)
	if err != nil {
		return fmt.Errorf("failed to process action command: %v", err)
	}

	fmt.Printf("\nExecuting action: %s\n", *action.Description)
	fmt.Printf("Command: %s\n", command)

	if err := run.ExecuteCommand(command); err != nil {
		return fmt.Errorf("failed to execute action: %v", err)
	}

	fmt.Printf("%s Action completed successfully %s\n", utils.CHECK, utils.CHECK)
	return nil
}

func handleRunWorkflow(workflowId string, cmd *cobra.Command) {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to initialize store: %v", err))
		os.Exit(1)
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	workflowStore := kv.NewWorkflowStore(store)

	workflow, err := workflowStore.GetWorkflow(workflowId)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get workflow: %v", err))
		os.Exit(1)
	}

	if workflow.UsesSudo {
		if os.Getuid() != 0 {
			utils.LogError("This workflow requires sudo privileges. Please run with sudo.")
			os.Exit(1)
		}
	}

	// Process variables from flags
	variables := make(map[string]string)
	varFlags, err := cmd.Flags().GetStringArray("var")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get variables: %v", err))
		os.Exit(1)
	}

	for _, v := range varFlags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			utils.LogError(fmt.Sprintf("Invalid variable format: %s. Use KEY=VALUE format", v))
			os.Exit(1)
		}
		variables[parts[0]] = parts[1]
	}

	// Process remaining variables if store_variables is true
	if workflow.Config.StoreVariables {
		reader := bufio.NewReader(os.Stdin)
		requiredVars := make(map[string]bool)

		// Collect variables from all sources
		for _, check := range workflow.PreChecks {
			vars := utils.ExtractTemplateVars(check.Command)
			for _, v := range vars {
				requiredVars[v] = true
			}
		}
		for _, step := range workflow.Steps {
			vars := utils.ExtractTemplateVars(step.Command)
			for _, v := range vars {
				requiredVars[v] = true
			}
		}
		for _, action := range workflow.Actions {
			vars := utils.ExtractTemplateVars(action.Command)
			for _, v := range vars {
				requiredVars[v] = true
			}
		}

		// Prompt for missing variables
		for v := range requiredVars {
			if _, exists := variables[v]; !exists {
				fmt.Printf("%s: ", v)
				value, err := reader.ReadString('\n')
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to read input: %v", err))
					os.Exit(1)
				}
				variables[v] = strings.TrimSpace(value)
			}
		}
	}

	// Run pre-checks
	if err := runPreChecks(workflow, variables); err != nil {
		os.Exit(1)
	}

	// Check if we're running specific actions
	actionFlags, err := cmd.Flags().GetStringArray("action")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get action flags: %v", err))
		os.Exit(1)
	}

	if len(actionFlags) > 0 {
		// Execute specified actions
		for _, actionName := range actionFlags {
			if err := executeAction(workflow, actionName, variables); err != nil {
				utils.LogError(fmt.Sprintf("Failed to execute action '%s': %v", actionName, err))
				os.Exit(1)
			}
		}
		utils.LogSuccess(fmt.Sprintf("Workflow actions completed successfully"))
		return
	}

	// Run all steps
	utils.ColorSizePrint("green", "small", fmt.Sprintf("\nExecuting workflow steps: %s\n\n", workflow.Name))
	for i, step := range workflow.Steps {
		command, err := utils.ApplyVariablesToCommand(step.Command, variables)
		if err != nil {
			utils.LogError(fmt.Sprintf("Failed to process step command: %v", err))
			os.Exit(1)
		}

		utils.ColorSizePrint("blue", "small", fmt.Sprintf("[%d]: %s\n", i+1, *step.Description))
		utils.ColorSizePrint("blue", "small", fmt.Sprintf("$_: %s\n\n", command))

		if err := run.ExecuteCommand(command); err != nil {
			utils.LogError(fmt.Sprintf("Failed to execute step %d: %v", i+1, err))
			os.Exit(1)
		}
	}

	utils.LogSuccess(fmt.Sprintf("Workflow '%s' completed successfully", workflow.Name))
}

func handleLoadRemoteTemplate(url string) error {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		return fmt.Errorf("failed to initialize kv store: %v", err)
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	templateStore := kv.NewTemplateStoreManager(store)

	content, err := utils.DownloadTemplate(url)
	if err != nil {
		return fmt.Errorf("failed to download template: %v", err)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nEnter template name: ")
	templateName, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read template name: %v", err)
	}

	templateName = strings.TrimSpace(templateName)
	if templateName == "" {
		return fmt.Errorf("template name cannot be empty")
	}

	slug := utils.FormatString(templateName)

	existing, err := templateStore.GetTemplate(slug)
	if err == nil && existing != nil {
		return fmt.Errorf("template with name '%s' already exists", slug)
	}

	variables := utils.ExtractTemplateVars(string(content))
	if len(variables) > 0 {
		utils.LogInfo("Template variables detected:")
		for _, v := range variables {
			fmt.Printf("  • %s\n", v)
		}
		fmt.Println()
	}

	template := kv.TemplateItem{
		Slug:     slug,
		Workflow: string(content),
	}

	if err := templateStore.CreateTemplate(template); err != nil {
		return fmt.Errorf("failed to create template: %v", err)
	}

	utils.LogSuccess(fmt.Sprintf("Template '%s' created successfully", slug))
	return nil
}

func handleWorkflowInfo(workflowId string) {
	kvDB, err := kv.InitDB("migraine")
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to initialize store: %v", err))
		os.Exit(1)
	}
	defer kvDB.Close()

	store := kv.New(kvDB)
	workflowStore := kv.NewWorkflowStore(store)

	workflow, err := workflowStore.GetWorkflow(workflowId)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get workflow: %v", err))
		os.Exit(1)
	}

	// Print workflow header
	fmt.Printf("\n%s%s%s (%s)\n", utils.BOLD, workflow.Name, utils.RESET, workflowId)
	if workflow.Description != nil && *workflow.Description != "" {
		utils.ColorPrint("gray", fmt.Sprintf("%s\n", *workflow.Description))
	}

	// Print Pre-checks
	if len(workflow.PreChecks) > 0 {
		fmt.Printf("\n%spre-checks:%s\n", utils.BOLD, utils.RESET)
		for i, check := range workflow.PreChecks {
			fmt.Printf("  %d. ", i+1)
			if check.Description != nil {
				fmt.Printf("%s\n", *check.Description)
			}
			utils.ColorSizePrint("blue", "small", fmt.Sprintf("     %s\n", check.Command))
		}
	}

	// Print Steps
	if len(workflow.Steps) > 0 {
		fmt.Printf("\n%ssteps:%s\n", utils.BOLD, utils.RESET)
		utils.ColorSizePrint("gray", "small", fmt.Sprintf("  run: mgr run %s\n\n", workflowId))
		for i, step := range workflow.Steps {
			fmt.Printf("  %d. ", i+1)
			if step.Description != nil {
				fmt.Printf("%s\n", *step.Description)
			}
			utils.ColorSizePrint("blue", "small", fmt.Sprintf("     %s\n", step.Command))
		}
	}

	// Print Actions
	if len(workflow.Actions) > 0 {
		fmt.Printf("\n%sactions:%s\n", utils.BOLD, utils.RESET)
		for name, action := range workflow.Actions {
			fmt.Printf("  %s%s%s\n", utils.BOLD, name, utils.RESET)
			if action.Description != nil {
				fmt.Printf("    %s\n", *action.Description)
			}
			utils.ColorSizePrint("blue", "small", fmt.Sprintf("    %s\n", action.Command))
			utils.ColorSizePrint("gray", "small", fmt.Sprintf("    run: mgr run %s -a %s\n", workflowId, name))
		}
	}

	// Print Variables
	vars := make(map[string]bool)

	// Extract variables from pre-checks
	for _, check := range workflow.PreChecks {
		for _, v := range utils.ExtractTemplateVars(check.Command) {
			vars[v] = true
		}
	}

	// Extract from steps
	for _, step := range workflow.Steps {
		for _, v := range utils.ExtractTemplateVars(step.Command) {
			vars[v] = true
		}
	}

	// Extract from actions
	for _, action := range workflow.Actions {
		for _, v := range utils.ExtractTemplateVars(action.Command) {
			vars[v] = true
		}
	}

	if len(vars) > 0 {
		fmt.Printf("\n%sRequired Variables:%s\n", utils.BOLD, utils.RESET)
		for v := range vars {
			fmt.Printf("  • %s\n", v)
			utils.ColorSizePrint("gray", "small", fmt.Sprintf("    Set with: -v %s=value\n", v))
		}
	}

	fmt.Println()
}
