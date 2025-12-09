package provider

import (
	"encoding/json"
	"fmt"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func decodeChatCompletionRequest(body []byte, request *chatCompletionRequest) error {
	if err := json.Unmarshal(body, request); err != nil {
		return fmt.Errorf("unable to unmarshal request: %v", err)
	}
	if request.Messages == nil || len(request.Messages) == 0 {
		return fmt.Errorf("no message found in the request body: %s", body)
	}
	return nil
}

func decodeEmbeddingsRequest(body []byte, request *embeddingsRequest) error {
	if err := json.Unmarshal(body, request); err != nil {
		return fmt.Errorf("unable to unmarshal request: %v", err)
	}
	return nil
}

func decodeImageGenerationRequest(body []byte, request *imageGenerationRequest) error {
	if err := json.Unmarshal(body, request); err != nil {
		return fmt.Errorf("unable to unmarshal request: %v", err)
	}
	return nil
}

func replaceJsonRequestBody(request interface{}) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("unable to marshal request: %v", err)
	}
	log.Debugf("request body: %s", string(body))
	err = proxywasm.ReplaceHttpRequestBody(body)
	if err != nil {
		return fmt.Errorf("unable to replace the original request body: %v", err)
	}
	return err
}

func replaceRequestBody(body []byte) error {
	log.Debugf("request body: %s", string(body))
	err := proxywasm.ReplaceHttpRequestBody(body)
	if err != nil {
		return fmt.Errorf("unable to replace the original request body: %v", err)
	}
	return nil
}

func insertContextMessage(request *chatCompletionRequest, content string) {
	fileMessage := chatMessage{
		Role:    roleSystem,
		Content: content,
	}
	var firstNonSystemMessageIndex int
	for i, message := range request.Messages {
		if message.Role != roleSystem {
			firstNonSystemMessageIndex = i
			break
		}
	}
	if firstNonSystemMessageIndex == 0 {
		request.Messages = append([]chatMessage{fileMessage}, request.Messages...)
	} else {
		request.Messages = append(request.Messages[:firstNonSystemMessageIndex], append([]chatMessage{fileMessage}, request.Messages[firstNonSystemMessageIndex:]...)...)
	}
}

func ReplaceResponseBody(body []byte) error {
	log.Debugf("response body: %s", string(body))
	err := proxywasm.ReplaceHttpResponseBody(body)
	if err != nil {
		return fmt.Errorf("unable to replace the original response body: %v", err)
	}
	return nil
}

func deleteNullValueFields(body []byte, fields []string) ([]byte, error) {
	if fields == nil || len(fields) == 0 {
		return body, nil
	}
	for _, field := range fields {
		if result := gjson.GetBytes(body, field); result.Exists() && result.Type == gjson.Null {
			// null value is not allowed for the given field, delete it from the request
			log.Debugf("[ai-proxy] null value found in field %s, removed it", field)
			if transformedBody, err := sjson.DeleteBytes(body, field); err != nil {
				return body, fmt.Errorf("[ai-proxy] failed to delete %s field with null value: %v", field, err)
			} else {
				body = transformedBody
			}
		}
	}
	return body, nil
}

func expandExtraBodyField(body []byte) []byte {
	extraBody := gjson.GetBytes(body, requestFieldExtraBody)
	if !extraBody.Exists() {
		return body
	}
	log.Debugf("azureProvider: expanding %s field into request body", requestFieldExtraBody)
	if extraBody.Type != gjson.JSON {
		log.Warnf("azureProvider: %s field is not a JSON object, skipping expansion", requestFieldExtraBody)
		return body
	}
	if cleanedBody, err := sjson.DeleteBytes(body, requestFieldExtraBody); err != nil {
		log.Warnf("azureProvider: failed to delete %s in request body, err: %v", requestFieldExtraBody, err)
		return body
	} else {
		body = cleanedBody
	}
	extraBody.ForEach(func(key, value gjson.Result) bool {
		log.Debugf("azureProvider: moving field %s into request body", key.String())
		if transformedBody, err := sjson.SetRawBytes(body, key.String(), []byte(value.Raw)); err != nil {
			log.Warnf("azureProvider: failed to set %s into request body, value: %s err: %v", key.String(), value.Raw, err)
		} else {
			body = transformedBody
		}
		return true
	})
	return body
}

func normalizeChatCompletionsRequest(body []byte) ([]byte, error) {
	if transformedBody, err := normalizeChatCompletionsMessages(body); err != nil {
		return body, fmt.Errorf("failed to normalize chat completions messages: %v", err)
	} else {
		return transformedBody, nil
	}
}

func normalizeChatCompletionsMessages(body []byte) ([]byte, error) {
	messageCount := int(gjson.GetBytes(body, "messages.#").Int())
	if messageCount == 0 {
		return body, nil
	}
	for i := 0; i < messageCount; i++ {
		if updatedBody, err := complementChatCompletionsMessageRole(body, i); err != nil {
			return body, fmt.Errorf("failed to complement role field in message %d: %v", i, err)
		} else {
			body = updatedBody
		}
		if updatedBody, err := fixChatCompletionsImageUrlValues(body, i); err != nil {
			return body, fmt.Errorf("failed to fix image url values in message %d: %v", i, err)
		} else {
			body = updatedBody
		}
	}
	return body, nil
}

func complementChatCompletionsMessageRole(body []byte, messageIndex int) ([]byte, error) {
	// Check if the role field exists and isn't empty in the message
	rolePath := fmt.Sprintf("messages.%d.role", messageIndex)
	if role := gjson.GetBytes(body, rolePath); !role.Exists() || role.String() == "" {
		// If the role field is missing or empty, set it to "assistant"
		log.Debugf("complementing missing role field in response body at path %s", rolePath)
		if updatedBody, err := sjson.SetBytes(body, rolePath, roleAssistant); err != nil {
			return body, fmt.Errorf("failed to set role field in response body: %v", err)
		} else {
			body = updatedBody
		}
	}
	return body, nil
}

func fixChatCompletionsImageUrlValues(body []byte, messageIndex int) ([]byte, error) {
	contentCount := int(gjson.GetBytes(body, fmt.Sprintf("messages.%d.content.#", messageIndex)).Int())
	if contentCount == 0 {
		return body, nil
	}
	for contentIndex := 0; contentIndex < contentCount; contentIndex++ {
		imageUrlPath := fmt.Sprintf("messages.%d.content.%d.image_url", messageIndex, contentIndex)
		if imageUrl := gjson.GetBytes(body, imageUrlPath); imageUrl.Exists() && imageUrl.Type == gjson.String {
			log.Debugf("convert image_url at path %s from string to object", imageUrlPath)
			if updatedBody, err := sjson.SetBytes(body, imageUrlPath+".url", imageUrl.Str); err != nil {
				return body, fmt.Errorf("failed to set %s field in response body: %v", imageUrlPath, err)
			} else {
				body = updatedBody
			}
		}
	}
	return body, nil
}
