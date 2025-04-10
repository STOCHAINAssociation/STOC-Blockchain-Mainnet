// SPDX-License-Identifier: MIT
pragma solidity ^0.8.4;

import "@klaytn/contracts/KIP/token/KIP7/KIP7.sol";
import "@klaytn/contracts/KIP/token/KIP7/extensions/KIP7Burnable.sol";
import "@klaytn/contracts/KIP/token/KIP7/extensions/KIP7Snapshot.sol";
import "@klaytn/contracts/access/Ownable.sol";
import "@klaytn/contracts/security/Pausable.sol";
import "@klaytn/contracts/KIP/token/KIP7/extensions/draft-KIP7Permit.sol";
import "@klaytn/contracts/KIP/token/KIP7/extensions/KIP7Votes.sol";
import "@klaytn/contracts/KIP/token/KIP7/extensions/KIP7FlashMint.sol";

contract MyToken is KIP7, KIP7Burnable, KIP7Snapshot, Ownable, Pausable, KIP7Permit, KIP7Votes, KIP7FlashMint {
    constructor() KIP7("STO Operating System Token", "STO OS") KIP7Permit("STO OS") {
        _mint(msg.sender, 1000000000 * 10 ** decimals());
    }

    function supportsInterface(bytes4 interfaceId)
        public
        view
        virtual
        override(KIP7, KIP7Burnable)
        returns (bool)
    {
        return
            interfaceId == type(IKIP7Permit).interfaceId ||
            interfaceId == type(IVotes).interfaceId ||
            super.supportsInterface(interfaceId);
    }

    function snapshot() public onlyOwner {
        _snapshot();
    }

    function pause() public onlyOwner {
        _pause();
    }

    function unpause() public onlyOwner {
        _unpause();
    }

    function mint(address to, uint256 amount) public onlyOwner {
        _mint(to, amount);
    }

    function _beforeTokenTransfer(address from, address to, uint256 amount)
        internal
        whenNotPaused
        override(KIP7, KIP7Snapshot)
    {
        super._beforeTokenTransfer(from, to, amount);
    }

    // The following functions are overrides required by Solidity.

    function _afterTokenTransfer(address from, address to, uint256 amount)
        internal
        override(KIP7, KIP7Votes)
    {
        super._afterTokenTransfer(from, to, amount);
    }

    function _mint(address to, uint256 amount) internal override(KIP7, KIP7Votes) {
        super._mint(to, amount);
    }

    function _burn(address account, uint256 amount)
        internal
        override(KIP7, KIP7Votes)
    {
        super._burn(account, amount);
    }
}