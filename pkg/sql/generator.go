// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package sql

import (
	"github.com/pkg/errors"
	"golang.org/x/net/context"

	"github.com/cockroachdb/cockroach/pkg/sql/parser"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlbase"
)

// valueGenerator represents a node that produces rows
// computationally, by means of a "generator function" (called
// "set-generating function" in PostgreSQL).
type valueGenerator struct {
	// expr holds the function call that needs to be performed,
	// including its arguments that need evaluation, to obtain the
	// generator object.
	expr parser.TypedExpr

	// gen is a reference to the generator object that produces the row
	// for this planNode.
	gen parser.ValueGenerator

	// columns is the signature of this generator.
	columns sqlbase.ResultColumns
}

// makeGenerator creates a valueGenerator instance that wraps a call to a
// generator function.
func (p *planner) makeGenerator(ctx context.Context, t *parser.FuncExpr) (planNode, error) {
	if err := p.parser.AssertNoAggregationOrWindowing(t, "FROM", p.session.SearchPath); err != nil {
		return nil, err
	}

	normalized, err := p.analyzeExpr(
		ctx, t, multiSourceInfo{}, parser.IndexedVarHelper{}, parser.TypeAny, false, "FROM",
	)
	if err != nil {
		return nil, err
	}

	tType, ok := normalized.ResolvedType().(parser.TTable)
	if !ok {
		return nil, errors.Errorf("FROM expression is not a generator: %s", t)
	}

	columns := make(sqlbase.ResultColumns, len(tType.Cols))
	for i := range columns {
		columns[i].Name = tType.Labels[i]
		columns[i].Typ = tType.Cols[i]
	}

	return &valueGenerator{
		expr:    normalized,
		columns: columns,
	}, nil
}

func (n *valueGenerator) Start(params runParams) error {
	expr, err := n.expr.Eval(&params.p.evalCtx)
	if err != nil {
		return err
	}
	var tb *parser.DTable
	if expr == parser.DNull {
		tb = parser.EmptyDTable()
	} else {
		tb = expr.(*parser.DTable)
	}

	gen := tb.ValueGenerator
	if err := gen.Start(); err != nil {
		return err
	}

	n.gen = gen
	return nil
}

func (n *valueGenerator) Next(params runParams) (bool, error) {
	if err := params.p.cancelChecker.Check(); err != nil {
		return false, err
	}
	return n.gen.Next()
}
func (n *valueGenerator) Values() parser.Datums { return n.gen.Values() }

func (n *valueGenerator) Close(context.Context) {
	if n.gen != nil {
		n.gen.Close()
	}
}
