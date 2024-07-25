# Graph RAG with ATO docs

## Context

I was doing my tax returns and realize that ATO provides a great corpus of documents that I could "train" play around with and recently learnt about Graph RAG and wanted to use this to play with a few ideas. These are

- How do I transform / scrub data better
- Can I use Go as my language to make API calls and transform data and be more performant than Python (Python took 35mins to fetch 2000 articles and insert 13,000 entities and 16,000 relationships.([Link](https://arc.net/l/quote/bdvblmoy)))
- How far can I get with the graph RAG approach

## Process

`docs/raw` contained the raw HTML from the ATO website (running `curl https://www.ato.gov.au/print/section/9eab54d3-6618-4bd4-aa96-a0c9fe7b130c` will get you it)

- I was so glad ATO has this in their "Print section" option, made life so much easier (HTML to MD is easier than PDF to MD)

`docs/cleaned` was what I got after playing with `pandoc` and using lua filters (`convert-md.yml` contains all the parameters I passed and `convert-md.lua` contains the filters I applied)

- To run it, I just ran `pandoc -d convert-md.yaml -i deductions-you-can-claim.html -o deductions-you-can-claim.md`
- Learnt a lot about parsing HTML with tables and filtering stuff out.

`split-to-chunks.go` was how I grab the markdown file and split them into sqlite.

- Had a go at "writing" iterators, I kind of understood stuff but need to dig deeper on how channels work in go
- Noticed on the HTML, all parts were split by this "QC....." followed by 5 digits (probably an internal ID for ATO), used that to split since it was a nice separation of content
- Used sqlite to store the split chunks

`embed-chunks.go`
- Had a go at using Go channels / concurrency model to speed up the OpenAI calls. I like it, much cleaner than Python's.
- Tried to use `sqlite-vss` but the support for Go is so sad T_T, so I just stored it as a string and manually calculate again
- Next time though, I want to try 2 different DB's (Cozo (datalog based) and Qdrant)

With the above work, I have the baseline RAG ready to go, below are work for the Graph RAG approach

`store-chunks-as-graph.go` basically loops through each chunk, calls OpenAI to make Cyhper calls and commit them to Neo4j

- Finally played with Tools / Function calling, it's pretty good! More reliable than "forcing" OpenAI to have a JSON schema through prompt engineering
- Resulted in this funky looking graph
[Graph of Neo4j](./graph-viz.png)

`ask-question.go` calculates the baseline RAG with rawdog cosine similarity (one day I'll get a vector database HAHA) and grabs just one (closest) document. This then gets compared with the graph RAG approach where OpenAI will create the cypher query based on the question and graph schema, get that context and create an answer

- The cypher query "works", but I noticed lots of duplicates (which nicely(?) the Microsoft blog mentions also happens) - Might look into [entity resolution](https://arc.net/l/quote/xfgqdovjc) next

```
Example of  duplication
  {
    "DeductibleGiftRecipient": "Deductible Gift Recipient",
    "GiftDonation": "Gifts and Donations"
  },
  {
    "DeductibleGiftRecipient": "Deductible Gift Recipient",
    "GiftDonation": "Gifts and Donations"
  },
  {
    "DeductibleGiftRecipient": "Deductible Gift Recipient",
    "GiftDonation": null
  },
  {
    "DeductibleGiftRecipient": "Deductible Gift Recipient",
    "GiftDonation": "Gifts and Donations"
  },
  {
    "DeductibleGiftRecipient": "Tax Adviser",
    "GiftDonation": "Gifts and Donations"
  },
  {
    "DeductibleGiftRecipient": "Tax Adviser",
    "GiftDonation": "Gifts and Donations"
  },
  {

```


## Results

Question: What are the eligibility requirements to claim a deduction?

Baseline RAG
```
To claim a deduction for work-related expenses, you must meet the following eligibility requirements:

1. **Personal Expense**: You must have spent the money yourself, and you were not reimbursed by your employer.

2. **Direct Relation to Income**: The expense must directly relate to earning your income.

3. **Record Keeping**: You must have a record (usually a receipt) to prove the expense.

Additionally, if the expense was incurred for both work and private purposes, you can only claim a deduction for the portion related to work. You cannot claim a deduction if your employer pays for or reimburses you for the expense.
```

Graph RAG
```
Based on the context provided, the following eligibility requirements appear to be associated with claiming deductions in the Australian tax context:

1. **Working from Home Deduction**:
   - Eligible for claims related to specific expenses such as:
     - Electricity expenses
     - Internet expenses
     - Mobile phone expenses
   - There is also a guideline that suggests a claim of “67 cents per hour” for working from home.

2. **Occupancy Expenses Claim**:
   - May require proof of employer involvement or specific employment conditions.

3. **Deduction for Expenses to Manage Your Tax Affairs**:
   - Endorsement involves lodging through a registered tax agent.

4. **Claim for ATO Interest**:
   - A specific deductible gift recipient related to ATO interest deduction might apply.

In general, to successfully claim deductions:
- Individuals must incur the expenses in the course of earning their income.
- Claims must be substantiated with adequate evidence, such as receipts or records.
- Users should follow the guidelines laid out by the Australian Taxation Office (ATO) and ensure they meet specific eligibility criteria based on the type of deduction being claimed.

Always refer to the latest ATO guidelines or consult with a registered tax agent for specific eligibility criteria and details surrounding each deduction type.
```

## Learnings

- Golang "works", not as fast for dev

## Next ideas to play with

- Graph RAG right now is super rough and naive, keen to learn some optimizations like
- [Entity Resolution](https://arc.net/l/quote/xfgqdovjc) 
- [Element Summarization](https://arc.net/l/quote/jiddlnlw)
