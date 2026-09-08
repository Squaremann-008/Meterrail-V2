/**
 * Envio HyperIndex event handlers.
 *
 * `generated` is produced by `envio codegen` from config.yaml and
 * schema.graphql — run `make indexer-codegen` before typechecking this file.
 * It does not exist in a fresh checkout, which is why it is gitignored.
 */
import { ERC20, type AccountBalance } from 'generated';

/** Composite key for a balance row: one per (chain, token, holder). */
function balanceId(chainId: number, contractAddress: string, account: string): string {
  return `${chainId}-${contractAddress.toLowerCase()}-${account.toLowerCase()}`;
}

/**
 * The zero address is the mint/burn counterparty. Tracking a balance for it
 * would produce a meaningless ever-growing negative number.
 */
const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000';

function isRealAccount(address: string): boolean {
  return address.toLowerCase() !== ZERO_ADDRESS;
}

ERC20.Transfer.handler(async ({ event, context }) => {
  const chainId = event.chainId;
  const contractAddress = event.srcAddress.toLowerCase();
  const from = event.params.from.toLowerCase();
  const to = event.params.to.toLowerCase();
  const value = event.params.value;

  context.Transfer.set({
    // The log is uniquely identified by chain, transaction and log index;
    // using that as the id makes re-indexing idempotent.
    id: `${chainId}_${event.transaction.hash}_${event.logIndex}`,
    chainId,
    blockNumber: BigInt(event.block.number),
    blockTimestamp: BigInt(event.block.timestamp),
    transactionHash: event.transaction.hash,
    logIndex: event.logIndex,
    contractAddress,
    from,
    to,
    value,
  });

  // Debit the sender, credit the recipient, skipping mints and burns.
  if (isRealAccount(from)) {
    await adjustBalance(context, chainId, contractAddress, from, -value, event.block.number);
  }
  if (isRealAccount(to)) {
    await adjustBalance(context, chainId, contractAddress, to, value, event.block.number);
  }
});

ERC20.Approval.handler(async ({ event, context }) => {
  context.Approval.set({
    id: `${event.chainId}_${event.transaction.hash}_${event.logIndex}`,
    chainId: event.chainId,
    blockNumber: BigInt(event.block.number),
    blockTimestamp: BigInt(event.block.timestamp),
    transactionHash: event.transaction.hash,
    logIndex: event.logIndex,
    contractAddress: event.srcAddress.toLowerCase(),
    owner: event.params.owner.toLowerCase(),
    spender: event.params.spender.toLowerCase(),
    value: event.params.value,
  });
});

/**
 * Applies a signed delta to a holder's running balance, creating the row on
 * first sight.
 *
 * The balance can go negative when the indexer starts mid-history: the first
 * observed event for an address may be an outbound transfer of tokens it
 * acquired before start_block. That is expected, and it is why this is a delta
 * rather than an authoritative balance — call `balanceOf` for that.
 */
async function adjustBalance(
  context: {
    AccountBalance: {
      get: (id: string) => Promise<AccountBalance | undefined>;
      set: (entity: AccountBalance) => void;
    };
  },
  chainId: number,
  contractAddress: string,
  account: string,
  delta: bigint,
  blockNumber: number,
): Promise<void> {
  const id = balanceId(chainId, contractAddress, account);
  const existing = await context.AccountBalance.get(id);

  context.AccountBalance.set({
    id,
    chainId,
    contractAddress,
    account,
    balance: (existing?.balance ?? 0n) + delta,
    transferCount: (existing?.transferCount ?? 0) + 1,
    lastBlockNumber: BigInt(blockNumber),
  });
}
