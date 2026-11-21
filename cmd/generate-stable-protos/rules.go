package main

import (
	"fmt"

	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/seq"
)

func validateDraftRules(tags draftTags, f *ir.File, endpointUses rpcEndpointUses, allowlist rpcEndpointPositions) error {
	for t := range seq.Values(f.AllTypes()) {
		if t.IsMapEntry() {
			continue
		}
		parent := t.Parent()
		if tags.has(t.Options()) && isDraftType(tags, parent) {
			return draftNestingError(t.FullName(), parent.FullName())
		}
		draft := isDraftType(tags, t)
		if t.IsEnum() {
			if err := validateDraftEnum(tags, t, draft); err != nil {
				return err
			}
			continue
		}

		for o := range seq.Values(t.Oneofs()) {
			if o.AST().IsZero() {
				continue
			}
			if tags.has(o.Options()) {
				if draft {
					return draftNestingError(o.FullName(), t.FullName())
				}
			} else if !draft && allDraft(o.Members(), func(m ir.Member) bool { return isDraftMember(tags, m) }) {
				for m := range seq.Values(o.Members()) {
					if tags.has(m.Options()) {
						return fmt.Errorf("%s: all oneof members are draft; set draft_oneof to a common feature and remove the member draft annotations", o.FullName())
					}
				}
				return fmt.Errorf("%s: all oneof members are draft; add draft_oneof", o.FullName())
			}
		}
		for m := range seq.Values(t.Members()) {
			if !tags.has(m.Options()) {
				continue
			}
			o := m.Oneof()
			if !o.AST().IsZero() && (draft || tags.has(o.Options())) {
				return draftNestingError(m.FullName(), o.FullName())
			}
			if draft {
				return draftNestingError(m.FullName(), t.FullName())
			}
		}
	}

	for ext := range seq.Values(f.AllExtensions()) {
		for _, ref := range []struct {
			role string
			typ  ir.Type
		}{
			{"value type", ext.Element()},
			{"extendee", ext.Container()},
			{"declaring parent", ext.Parent()},
		} {
			if isDraftType(tags, ref.typ) {
				return fmt.Errorf("%s: extension references draft type %s as its %s", ext.FullName(), ref.typ.FullName(), ref.role)
			}
		}
	}

	for s := range seq.Values(f.Services()) {
		if err := validateDraftService(tags, s, endpointUses, allowlist); err != nil {
			return err
		}
	}
	return nil
}

func draftNestingError(name, container ir.FullName) error {
	return fmt.Errorf("%s: draft annotation is not allowed inside draft container %s", name, container)
}

func allDraft[T any](members seq.Indexer[T], isDraft func(T) bool) bool {
	for m := range seq.Values(members) {
		if !isDraft(m) {
			return false
		}
	}
	return members.Len() > 0
}

func validateDraftEnum(tags draftTags, enum ir.Type, draft bool) error {
	if !draft && allDraft(enum.Members(), func(m ir.Member) bool { return tags.has(m.Options()) }) {
		return fmt.Errorf("%s: all enum values are draft; set draft_enum to a common feature and remove the draft_enum_value annotations", enum.FullName())
	}
	byNumber := make(map[int32]ir.Member)
	for value := range seq.Values(enum.Members()) {
		valueDraft := tags.has(value.Options())
		if valueDraft && draft {
			return draftNestingError(value.FullName(), enum.FullName())
		}
		if valueDraft && value.Number() == 0 {
			return fmt.Errorf("%s: draft enum value in stable enum %s cannot have number zero", value.FullName(), enum.FullName())
		}
		if other, ok := byNumber[value.Number()]; ok && (valueDraft || tags.has(other.Options())) {
			return fmt.Errorf("%s: enum value aliases %s at number %d in %s; aliases involving draft enum values are not allowed",
				value.FullName(), other.FullName(), value.Number(), enum.FullName())
		}
		byNumber[value.Number()] = value
	}
	return nil
}

func validateDraftService(tags draftTags, service ir.Service, endpointUses rpcEndpointUses, allowlist rpcEndpointPositions) error {
	serviceFeature := tags.feature(service.Options())
	if serviceFeature == "" && allDraft(service.Methods(), func(m ir.Method) bool { return tags.has(m.Options()) }) {
		return fmt.Errorf("%s: all RPCs are draft; set draft_service to a common feature, remove the draft_method annotations, and set each request and response type's explicit draft_message to the service feature", service.FullName())
	}
	for method := range seq.Values(service.Methods()) {
		feature, annotation := tags.feature(method.Options()), "draft_method"
		if serviceFeature != "" {
			if feature != "" {
				return fmt.Errorf("%s: draft_method is not allowed inside draft_service %s", method.FullName(), service.FullName())
			}
			feature, annotation = serviceFeature, "draft_service"
		}
		input, _ := method.Input()
		output, _ := method.Output()
		if input.FullName() == output.FullName() {
			return fmt.Errorf("%s: RPC uses %s as both request and response", method.FullName(), input.FullName())
		}
		for _, endpoint := range []struct {
			role string
			typ  ir.Type
		}{
			{"request", input},
			{"response", output},
		} {
			name := endpoint.typ.FullName()
			if feature == "" {
				if isDraftType(tags, endpoint.typ) {
					if !tags.has(endpoint.typ.Options()) {
						return fmt.Errorf("%s: RPC references draft type %s as its %s; inherited draft status is insufficient, so use a type that explicitly carries draft_message and give draft_method and both endpoints the same feature", method.FullName(), name, endpoint.role)
					}
					return fmt.Errorf("%s: RPC references draft type %s as its %s and requires draft_method", method.FullName(), name, endpoint.role)
				}
			} else if actual := tags.feature(endpoint.typ.Options()); actual != feature {
				if actual == "" {
					return fmt.Errorf("%s: %s requires %s type %s to explicitly carry draft_message = %q",
						method.FullName(), annotation, endpoint.role, name, feature)
				}
				return fmt.Errorf("%s: %s requires %s type %s to explicitly carry draft_message = %q; found %q",
					method.FullName(), annotation, endpoint.role, name, feature, actual)
			}
			use := rpcEndpointUse{position: rpcPosition{method.FullName(), endpoint.role}, stable: feature == ""}
			if err := endpointUses.add(name, use, allowlist); err != nil {
				return err
			}
		}
		if feature == "" {
			if err := validateMethodFieldReferences(tags, method, input, output); err != nil {
				return err
			}
		}
	}
	return nil
}

type rpcPosition struct {
	method ir.FullName
	role   string
}

type rpcEndpointUse struct {
	position rpcPosition
	stable   bool
}

type rpcEndpointPositions map[ir.FullName]map[rpcPosition]struct{}

type rpcEndpointUses map[ir.FullName]rpcPosition

func (uses rpcEndpointUses) add(name ir.FullName, use rpcEndpointUse, allowlist rpcEndpointPositions) error {
	if allowed, listed := allowlist[name]; listed {
		if !use.stable {
			return fmt.Errorf("%s %s: %s is allowlisted only for stable RPC endpoints", use.position.method, use.position.role, name)
		}
		if _, ok := allowed[use.position]; !ok {
			return fmt.Errorf("%s %s: %s is not allowlisted at this RPC endpoint position", use.position.method, use.position.role, name)
		}
		return nil
	}
	first, exists := uses[name]
	if !exists {
		uses[name] = use.position
		return nil
	}
	return fmt.Errorf("%s: RPC endpoint type is reused at %s %s and %s %s", name,
		first.method, first.role, use.position.method, use.position.role)
}
