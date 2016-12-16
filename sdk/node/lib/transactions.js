const shared = require('./shared')
const errors = require('./errors')

// TODO: replace with default handler in requestSingle/requestBatch variants
function checkForError(resp) {
  if ('code' in resp) {
    throw errors.create(
      errors.types.BAD_REQUEST,
      errors.formatErrMsg(resp, ''),
      {body: resp}
    )
  }
  return resp
}

class TransactionBuilder {
  constructor() {
    this.actions = []
  }

  baseTransaction(raw_tx) {
    this.base_transaction = raw_tx
  }

  issue(params) {
    this.actions.push(Object.assign({}, params, {type: 'issue'}))
  }

  controlWithAccount(params) {
    this.actions.push(Object.assign({}, params, {type: 'control_account'}))
  }

  controlWithProgram(params) {
    this.actions.push(Object.assign({}, params, {type: 'control_program'}))
  }

  spendFromAccount(params) {
    this.actions.push(Object.assign({}, params, {type: 'spend_account'}))
  }

  spendUnspentOutput(params) {
    this.actions.push(Object.assign({}, params, {type: 'spend_account_unspent_output'}))
  }

  retire(params) {
    this.actions.push(Object.assign({}, params, {type: 'control_program', control_program: '6a'}))
  }
}

module.exports = (client) => {
  return {
    query: (params) => shared.query(client, this, '/list-transactions', params),
    // this.queryAll = (params, processor) => shared.queryAll(this, params, processor)
    build: (builderBlock) => {
      const builder = new TransactionBuilder()
      builderBlock(builder)

      return client.request('/build-transaction', [builder])
        .then(resp => checkForError(resp[0]))
    },
    buildBatch: (builderBlocks) => {
      const builders = builderBlocks.map((builderBlock) => {
        const builder = new TransactionBuilder()
        builderBlock(builder)
        return builder
      })

      return shared.createBatch(client, '/build-transaction', builders)
    },
    submit: (signed) => {
      return client.request('/submit-transaction', {transactions: [signed]})
        .then(resp => checkForError(resp[0]))
    },
    submitBatch: (signed) => {
      return client.request('/submit-transaction', {transactions: signed})
        .then(resp => {
          return {
            successes: resp.map((item) => item.code ? null : item),
            errors: resp.map((item) => item.code ? item : null),
            response: resp,
          }
        })
    }
  }
}
