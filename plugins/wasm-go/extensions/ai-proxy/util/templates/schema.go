package templates

import (
	"encoding/json"
	"strings"
)

// queueItem represents an item in the processing queue
type queueItem struct {
	node       interface{}
	parent     interface{} // can be map[string]interface{} or []interface{}
	key        interface{} // can be string (for map) or int (for slice)
	activeDefs map[string]interface{}
	refChain   map[string]bool
}

// UnpackDefs expands *all* $ref entries pointing into $defs / definitions.
//
// This utility walks the entire schema tree (maps and slices) so it naturally
// resolves references hidden under any keyword – items, allOf, anyOf, oneOf,
// additionalProperties, etc.
//
// It mutates schema in-place and does not return anything. The helper keeps
// memory overhead low by resolving nodes as it encounters them rather than
// materialising a fully dereferenced copy first.
//
// Example usage:
//
//	schema := map[string]interface{}{
//	    "type": "object",
//	    "properties": map[string]interface{}{
//	        "user": map[string]interface{}{"$ref": "#/$defs/User"},
//	    },
//	    "$defs": map[string]interface{}{
//	        "User": map[string]interface{}{
//	            "type": "object",
//	            "properties": map[string]interface{}{
//	                "name": map[string]interface{}{"type": "string"},
//	            },
//	        },
//	    },
//	}
//	UnpackDefs(schema, map[string]interface{}{})
//	// Now schema["properties"]["user"] contains the resolved User definition
func UnpackDefs(schema map[string]interface{}, defs map[string]interface{}) {
	// Combine the defs handed down by the caller with defs/definitions found on
	// the current node. Local keys shadow parent keys to match JSON-schema
	// scoping rules.
	rootDefs := make(map[string]interface{})
	for k, v := range defs {
		rootDefs[k] = v
	}
	if schemaDefs, ok := schema["$defs"].(map[string]interface{}); ok {
		for k, v := range schemaDefs {
			rootDefs[k] = v
		}
	}
	if definitions, ok := schema["definitions"].(map[string]interface{}); ok {
		for k, v := range definitions {
			rootDefs[k] = v
		}
	}

	// Use iterative approach with queue to avoid recursion
	// Each item in queue is (node, parent_container, key/index, active_defs, ref_chain)
	queue := []queueItem{
		{
			node:       schema,
			parent:     nil,
			key:        nil,
			activeDefs: rootDefs,
			refChain:   make(map[string]bool),
		},
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		node := item.node
		parent := item.parent
		key := item.key
		activeDefs := item.activeDefs
		refChain := item.refChain

		// ----------------------------- dict (map) -----------------------------
		if nodeMap, ok := node.(map[string]interface{}); ok {
			// --- Case 1: this node *is* a reference ---
			if refStr, hasRef := nodeMap["$ref"].(string); hasRef {
				// Extract ref name (last part after /)
				parts := strings.Split(refStr, "/")
				refName := parts[len(parts)-1]

				// Check for circular reference in the resolution chain
				if refChain[refName] {
					// Circular reference detected - leave as-is to prevent infinite recursion
					continue
				}

				targetSchema, exists := activeDefs[refName]
				// Unknown reference – leave untouched
				if !exists {
					continue
				}

				targetMap, ok := targetSchema.(map[string]interface{})
				if !ok {
					continue
				}

				// Merge defs from the target to capture nested definitions
				childDefs := make(map[string]interface{})
				for k, v := range activeDefs {
					childDefs[k] = v
				}
				if targetDefs, ok := targetMap["$defs"].(map[string]interface{}); ok {
					for k, v := range targetDefs {
						childDefs[k] = v
					}
				}
				if targetDefinitions, ok := targetMap["definitions"].(map[string]interface{}); ok {
					for k, v := range targetDefinitions {
						childDefs[k] = v
					}
				}

				// Replace the reference with resolved copy
				resolved := deepCopy(targetMap)

				if parent != nil && key != nil {
					if parentMap, ok := parent.(map[string]interface{}); ok {
						if keyStr, ok := key.(string); ok {
							parentMap[keyStr] = resolved
						}
					} else if parentSlice, ok := parent.([]interface{}); ok {
						if keyInt, ok := key.(int); ok {
							parentSlice[keyInt] = resolved
						}
					}
				} else {
					// This is the root schema itself
					for k := range nodeMap {
						delete(nodeMap, k)
					}
					if resolvedMap, ok := resolved.(map[string]interface{}); ok {
						for k, v := range resolvedMap {
							nodeMap[k] = v
						}
						resolved = nodeMap
					}
				}

				// Add to ref chain to track circular references
				newRefChain := make(map[string]bool)
				for k, v := range refChain {
					newRefChain[k] = v
				}
				newRefChain[refName] = true

				// Add resolved node to queue for further processing
				queue = append(queue, queueItem{
					node:       resolved,
					parent:     parent,
					key:        key,
					activeDefs: childDefs,
					refChain:   newRefChain,
				})
				continue
			}

			// --- Case 2: regular map – process its values ---
			// Update defs with any nested $defs/definitions present *here*.
			currentDefs := make(map[string]interface{})
			for k, v := range activeDefs {
				currentDefs[k] = v
			}
			if nodeDefs, ok := nodeMap["$defs"].(map[string]interface{}); ok {
				for k, v := range nodeDefs {
					currentDefs[k] = v
				}
			}
			if nodeDefinitions, ok := nodeMap["definitions"].(map[string]interface{}); ok {
				for k, v := range nodeDefinitions {
					currentDefs[k] = v
				}
			}

			// Add all map values to queue
			for k, v := range nodeMap {
				queue = append(queue, queueItem{
					node:       v,
					parent:     nodeMap,
					key:        k,
					activeDefs: currentDefs,
					refChain:   refChain,
				})
			}

		} else if nodeSlice, ok := node.([]interface{}); ok {
			// ---------------------------- list (slice) ------------------------------
			// Add all slice items to queue
			for idx, item := range nodeSlice {
				queue = append(queue, queueItem{
					node:       item,
					parent:     nodeSlice,
					key:        idx,
					activeDefs: activeDefs,
					refChain:   refChain,
				})
			}
		}
	}
}

// deepCopy creates a deep copy of a value using JSON marshal/unmarshal
func deepCopy(src interface{}) interface{} {
	data, err := json.Marshal(src)
	if err != nil {
		return src
	}

	var dst interface{}
	if err := json.Unmarshal(data, &dst); err != nil {
		return src
	}

	return dst
}
