# The command line interface's fixtures

Most of this package's tests write the model they need into a temporary
directory, because a five-line model whose text is beside the assertion is
easier to read than one two directories away. The fixture here is the exception,
and it is the exception for a reason.

## `budget`

The representative model the token budget is measured against. It is a
two-storey office building: a registry declaring ten types and seven predicates,
a building with two storeys and two occupancy zones, thirteen spaces per storey
divided by partitions, and the surveyed corners, edges and rings each space's
outline is assembled from. Adjacent spaces share an edge rather than a
coordinate, and four corners per storey carry a corrected claim beside the
deprecated one it replaced.

| Path                     | Holds                                                             |
|--------------------------|-------------------------------------------------------------------|
| `registry.dfc`           | The whole vocabulary: types, predicates, one tolerance, two frames and the measurement relating them. |
| `entities/building.dfc`  | The building, its storeys and the zones occupancy is counted over. |
| `entities/level-0N.dfc`  | The spaces of one storey and the elements dividing them.           |
| `geometry/level-0N.dfc`  | The vertices, edges and loops of one storey.                       |

It is committed rather than generated because what is measured has to be a model
somebody could have authored. A generator would make the size a parameter, and a
budget measured against a parameter is a budget measured against whatever the
author chose — the number would move without anything about the engine changing,
and a reviewer could not tell which had happened.

It is also larger than anything else under `testdata` on purpose. A four-node
fixture would report the discovery calls at their very best: `list-types` costs
what the registry costs whatever the model holds, and `list-instances` costs
what a type's instance count costs. Measuring both against a model with one of
each would answer a question nobody asked.

The engine's own loader states its size, so nothing here has to be kept in step
by hand — the line in [`docs/token-budget.md`](../../../docs/token-budget.md)
beginning "which holds" is `Graph.Summary()` on this tree.

Nothing else measures against it, and nothing here is a golden: the fixture is
an input, and the output it produces is recorded in that document.
