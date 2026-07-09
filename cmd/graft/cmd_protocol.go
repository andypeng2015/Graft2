package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/odvcencio/graft/pkg/remote"
	"github.com/spf13/cobra"
)

type JSONProtocolOutput struct {
	SchemaVersion       int                            `json:"schemaVersion,omitempty"`
	ProtocolVersion     string                         `json:"protocolVersion"`
	Documentation       string                         `json:"documentation"`
	BaseURLFormat       string                         `json:"baseUrlFormat"`
	DefaultOrchardHost  string                         `json:"defaultOrchardHost"`
	HashFunction        string                         `json:"hashFunction"`
	Headers             []remote.ProtocolHeader        `json:"headers"`
	ClientCapabilities  []string                       `json:"clientCapabilities"`
	DefinedCapabilities []remote.ProtocolCapability    `json:"definedCapabilities"`
	Transports          []remote.ProtocolTransport     `json:"transports"`
	ServerLimits        []remote.ProtocolLimit         `json:"serverLimits"`
	ResponseLimits      []remote.ProtocolResponseLimit `json:"responseLimits"`
	Endpoints           []remote.ProtocolEndpoint      `json:"endpoints"`
	ObjectTypes         []string                       `json:"objectTypes"`
	ErrorShape          remote.ProtocolErrorShape      `json:"errorShape"`
}

func newProtocolCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "protocol",
		Short: "Show the supported remote protocol contract",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := jsonProtocolOutput(remote.SupportedProtocolContract())
			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), out)
			}
			printProtocolContract(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func jsonProtocolOutput(contract remote.ProtocolContract) JSONProtocolOutput {
	return JSONProtocolOutput{
		ProtocolVersion:     contract.ProtocolVersion,
		Documentation:       contract.Documentation,
		BaseURLFormat:       contract.BaseURLFormat,
		DefaultOrchardHost:  contract.DefaultOrchardHost,
		HashFunction:        contract.HashFunction,
		Headers:             contract.Headers,
		ClientCapabilities:  contract.ClientCapabilities,
		DefinedCapabilities: contract.DefinedCapabilities,
		Transports:          contract.Transports,
		ServerLimits:        contract.ServerLimits,
		ResponseLimits:      contract.ResponseLimits,
		Endpoints:           contract.Endpoints,
		ObjectTypes:         contract.ObjectTypes,
		ErrorShape:          contract.ErrorShape,
	}
}

func printProtocolContract(w io.Writer, out JSONProtocolOutput) {
	fmt.Fprintf(w, "Graft protocol %s\n", out.ProtocolVersion)
	fmt.Fprintf(w, "Documentation: %s\n", out.Documentation)
	fmt.Fprintf(w, "Base URL: %s\n", out.BaseURLFormat)
	fmt.Fprintf(w, "Default host: %s\n", out.DefaultOrchardHost)
	fmt.Fprintf(w, "Hash: %s\n", out.HashFunction)
	fmt.Fprintf(w, "Client capabilities: %s\n\n", strings.Join(out.ClientCapabilities, ", "))

	fmt.Fprintln(w, "Headers:")
	for _, header := range out.Headers {
		required := "optional"
		if header.Required {
			required = "required"
		}
		fmt.Fprintf(w, "  %-18s %-16s %-8s %s\n", header.Name, header.Direction, required, header.Description)
	}

	fmt.Fprintln(w, "\nEndpoints:")
	for _, endpoint := range out.Endpoints {
		fmt.Fprintf(w, "  %-4s %-45s %-12s %s\n", endpoint.Method, endpoint.Path, endpoint.Scope, endpoint.Description)
	}

	fmt.Fprintln(w, "\nServer limit keys:")
	for _, limit := range out.ServerLimits {
		fmt.Fprintf(w, "  %-12s %-8s %s\n", limit.Name, limit.Type, limit.Description)
	}

	fmt.Fprintln(w, "\nClient response read limits:")
	for _, limit := range out.ResponseLimits {
		fmt.Fprintf(w, "  %-16s %10d  %s\n", limit.Name, limit.Bytes, limit.Description)
	}

	fmt.Fprintf(w, "\nError JSON: {%q: string, %q: string, %q: string}\n",
		out.ErrorShape.CodeField, out.ErrorShape.MessageField, out.ErrorShape.DetailField)
}
