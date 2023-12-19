package appmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/serverv2/core/appmanager"
	"github.com/cosmos/cosmos-sdk/serverv2/core/transaction"
)

func NewSTFBuilder[T transaction.Tx]() *STFBuilder[T] {
	return &STFBuilder[T]{
		err:                nil,
		msgRouterBuilder:   newMsgRouterBuilder(),
		queryRouterBuilder: newMsgRouterBuilder(),
		txValidators:       make(map[string]func(ctx context.Context, tx T) error),
		beginBlockers:      make(map[string]func(ctx context.Context) error),
		endBlockers:        make(map[string]func(ctx context.Context) error),
	}
}

type STFBuilder[T transaction.Tx] struct {
	err error

	msgRouterBuilder   *msgRouterBuilder
	queryRouterBuilder *msgRouterBuilder
	txValidators       map[string]func(ctx context.Context, tx T) error
	beginBlockers      map[string]func(ctx context.Context) error
	endBlockers        map[string]func(ctx context.Context) error
	valSetUpdate       func(ctx context.Context) (appmanager.ValidatorUpdate, error)
	txCodec            transaction.Codec[T]
}

type STFBuilderOptions struct {
	// OrderEndBlockers can be optionally provided to set the order of end blockers.
	OrderEndBlockers []string
	// OrderBeginBlockers can be optionally provided to set the order of begin blockers.
	OrderBeginBlockers []string
	// OrderTxValidators can be optionally provided to set the order of tx validators.
	OrderTxValidators []string
}

func (s *STFBuilder[T]) Build(opts *STFBuilderOptions) (*STFAppManager[T], error) {
	msgHandler, err := s.msgRouterBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("unable to build msg handler: %w", err)
	}
	queryHandler, err := s.queryRouterBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("unable to build query handler: %w", err)
	}
	beginBlocker, err := s.makeBeginBlocker(opts.OrderBeginBlockers)
	if err != nil {
		return nil, fmt.Errorf("unable to build begin blocker: %w", err)
	}
	endBlocker, err := s.makeEndBlocker(opts.OrderEndBlockers)
	if err != nil {
		return nil, fmt.Errorf("unable to build end blocker: %w", err)
	}
	txValidator, err := s.makeTxValidator(opts.OrderTxValidators)
	if err != nil {
		return nil, fmt.Errorf("unable to build tx validator: %w", err)
	}
	return &STFAppManager[T]{
		handleMsg:      msgHandler,
		handleQuery:    queryHandler,
		doBeginBlock:   beginBlocker,
		doEndBlock:     endBlocker,
		doTxValidation: txValidator,
		decodeTx: func(txBytes []byte) (T, error) {
			return s.txCodec.Decode(txBytes)
		},
		branch: nil, // TODO
	}, nil
}

func (s *STFBuilder[T]) AddModules(modules ...appmanager.STFModule[T]) {
	for _, mod := range modules {
		s.AddModule(mod)
	}
}

func (s *STFBuilder[T]) AddModule(m appmanager.Module[T]) {
	// TODO: the best is add modules but not build them here but build them later when we call STFBuilder.Build.
	// build msg handler
	moduleMsgRouter := _newModuleMsgRouter(m.Name(), s.msgRouterBuilder)
	m.RegisterMsgHandlers(moduleMsgRouter)
	m.RegisterPreMsgHandler(moduleMsgRouter)
	m.RegisterPostMsgHandler(moduleMsgRouter)
	// build query handler
	moduleQueryRouter := _newModuleMsgRouter(m.Name(), s.queryRouterBuilder)
	m.RegisterQueryHandler(moduleQueryRouter)
	// add begin blockers and endblockers
	// TODO: check if is not nil, etc.
	s.beginBlockers[m.Name()] = m.BeginBlocker()
	s.endBlockers[m.Name()] = m.EndBlocker()
	s.txValidators[m.Name()] = m.TxValidator()
}

func (s *STFBuilder[T]) makeEndBlocker(order []string) (func(ctx context.Context) error, error) {
	// TODO do ordering...
	// TODO do checks if all are present etc
	return func(ctx context.Context) error {
		for module, f := range s.endBlockers {
			err := f(ctx)
			if err != nil {
				return fmt.Errorf("endblocker of module %s failure: %w", module, err)
			}
		}
		return nil
	}, nil
}

func (s *STFBuilder[T]) makeBeginBlocker(order []string) (func(ctx context.Context) error, error) {
	// TODO do ordering...
	// TODO do checks if all are present etc
	return func(ctx context.Context) error {
		for module, f := range s.beginBlockers {
			err := f(ctx)
			if err != nil {
				return fmt.Errorf("beginblocker of module %s failure: %w", module, err)
			}
		}
		return nil
	}, nil
}

func (s *STFBuilder[T]) makeTxValidator(order []string) (func(ctx context.Context, tx T) error, error) {
	// TODO do ordering...
	// TODO do checks if all are present etc
	return func(ctx context.Context, tx T) error {
		for module, f := range s.txValidators {
			err := f(ctx, tx)
			if err != nil {
				return fmt.Errorf("tx validation failed for module %s: %w", module, err)
			}
		}
		return nil
	}, nil
}

// we create some intermediary type that associates a registration error with the module.
var (
	_ appmanager.MsgRouterBuilder     = (*_moduleMsgRouter)(nil)
	_ appmanager.PostMsgRouterBuilder = (*_moduleMsgRouter)(nil)
	_ appmanager.PreMsgRouterBuilder  = (*_moduleMsgRouter)(nil)
)

func _newModuleMsgRouter(moduleName string, router *msgRouterBuilder) *_moduleMsgRouter {
	return &_moduleMsgRouter{
		err:              nil,
		moduleName:       moduleName,
		msgRouterBuilder: router,
	}
}

type _moduleMsgRouter struct {
	err              error
	moduleName       string
	msgRouterBuilder *msgRouterBuilder
}

func (r *_moduleMsgRouter) RegisterPreHandler(msg appmanager.Type, preHandler func(ctx context.Context, msg appmanager.Type) error) {
	r.msgRouterBuilder.RegisterPreHandler(TypeName(msg), preHandler)
}

func (r *_moduleMsgRouter) RegisterPostHandler(msg appmanager.Type, postHandler func(ctx context.Context, msg, msgResp appmanager.Type) error) {
	r.msgRouterBuilder.RegisterPostHandler(TypeName(msg), postHandler)
}

func (r *_moduleMsgRouter) RegisterHandler(msg appmanager.Type, handlerFunc func(ctx context.Context, msg appmanager.Type) (resp appmanager.Type, err error)) {
	typeName := TypeName(msg)
	err := r.msgRouterBuilder.RegisterHandler(typeName, handlerFunc)
	if err != nil {
		r.err = errors.Join(r.err, fmt.Errorf("%w: %s", err, r.moduleName))
	}
}
